package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type dependencies struct {
	YTDLP       string `json:"ytdlp"`
	FFmpeg      string `json:"ffmpeg"`
	FFprobe     string `json:"ffprobe"`
	Runtime     string `json:"runtime"`
	RuntimePath string `json:"runtime_path"`
}
type probeResult struct {
	stdout, stderr string
	code           int
}
type probeRunner func(context.Context, string, []string) (probeResult, error)
type boundedOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := 64*1024 - len(b.data)
	if len(p) > room {
		b.overflow = true
		p = p[:room]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func runProbe(ctx context.Context, path string, args []string) (probeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	// Inherit CORE's process group. CORE kills descendants on cancellation
	// and normal backend exit; a private group would escape that supervision.
	cmd.Env = setEnv(os.Environ(), "PYTHONDONTWRITEBYTECODE", "1")
	out, errout := &boundedOutput{}, &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = errout
	err := cmd.Run()
	if ctx.Err() != nil {
		return probeResult{}, ctx.Err()
	}
	if out.overflow || errout.overflow {
		return probeResult{}, fmt.Errorf("dependency output exceeds 64 KiB")
	}
	result := probeResult{stdout: string(out.data), stderr: string(errout.data)}
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			result.code = e.ExitCode()
		} else {
			return result, fmt.Errorf("dependency process failed: %w", err)
		}
	}
	return result, nil
}
func findExecutable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n:") {
		return "", fmt.Errorf("dependency requires absolute PATH entry")
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("regular executable required")
	}
	return path, nil
}
func (d dependencies) validate() error {
	if d.Runtime != "node" && d.Runtime != "deno" {
		return fmt.Errorf("unsupported dependency runtime")
	}
	for _, p := range []string{d.YTDLP, d.FFmpeg, d.FFprobe, d.RuntimePath} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.ContainsAny(p, "\x00\r\n:") {
			return fmt.Errorf("invalid recorded dependency path")
		}
	}
	return nil
}

var ytVersionRE = regexp.MustCompile(`^(\d{4}\.\d{2}\.\d{2})(?:[.a-zA-Z0-9+_-]*)$`)
var runtimeVersionRE = regexp.MustCompile(`^(?:deno |v)?([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)
var ejsRE = regexp.MustCompile(`\b(?:yt_dlp_ejs|yt-dlp-ejs)-[0-9]`)

func (b *backend) dependencies(ctx context.Context) (dependencies, error) {
	var d dependencies
	lookup := func(name string) (string, error) {
		p, err := b.look(name)
		if err != nil {
			return "", fmt.Errorf("missing %s on absolute PATH", name)
		}
		for _, base := range []string{b.atmRoot, b.localRoot} {
			if p == base || strings.HasPrefix(p, base+"/") {
				return "", fmt.Errorf("managed player/shim cannot be a playback dependency")
			}
		}
		return p, nil
	}
	var err error
	if d.FFmpeg, err = lookup("ffmpeg"); err != nil {
		return d, err
	}
	if d.FFprobe, err = lookup("ffprobe"); err != nil {
		return d, err
	}
	if d.YTDLP, err = lookup("yt-dlp"); err != nil {
		return d, err
	}
	isolation := []string{"--ignore-config", "--no-plugin-dirs", "--no-cache-dir", "--no-remote-components"}
	out, err := b.run(ctx, d.YTDLP, append(append([]string{}, isolation...), "--version"))
	if err != nil {
		return d, err
	}
	m := ytVersionRE.FindStringSubmatch(strings.TrimSpace(out.stdout))
	if out.code != 0 || len(m) != 2 {
		return d, fmt.Errorf("unreadable yt-dlp version")
	}
	date, err := time.Parse("2006.01.02", m[1])
	if err != nil || date.Before(time.Date(2025, 11, 12, 0, 0, 0, 0, time.UTC)) {
		return d, fmt.Errorf("yt-dlp >=2025.11.12 with matching EJS package required")
	}
	for _, candidate := range []struct{ name, min string }{{"deno", "2.3.0"}, {"node", "22.0.0"}} {
		path, e := lookup(candidate.name)
		if e != nil {
			continue
		}
		out, e = b.run(ctx, path, []string{"--version"})
		if e != nil {
			return d, e
		}
		if out.code != 0 {
			return d, fmt.Errorf("%s version probe exited unsuccessfully", candidate.name)
		}
		m := runtimeVersionRE.FindStringSubmatch(out.stdout)
		if out.code == 0 && len(m) == 2 && versionRE.MatchString(m[1]) && !newer(candidate.min, m[1]) {
			d.Runtime = candidate.name
			d.RuntimePath = path
			break
		}
	}
	if d.Runtime == "" {
		return d, fmt.Errorf("Deno >=2.3.0 or Node >=22.0.0 required")
	}
	args := append(append([]string{}, isolation...), "--verbose", "--simulate", "--no-js-runtimes", "--js-runtimes", d.Runtime+":"+d.RuntimePath)
	out, err = b.run(ctx, d.YTDLP, args)
	if err != nil {
		return d, err
	}
	runtimeRE := regexp.MustCompile(`(?m)^\[debug\] JS runtimes:.*\b` + d.Runtime + `-[0-9]`)
	if out.code != 2 || !ejsRE.MatchString(out.stderr) || !runtimeRE.MatchString(out.stderr) {
		return d, fmt.Errorf("yt-dlp did not report local EJS and selected JS runtime; update yt-dlp[default] and runtime")
	}
	if err = d.validate(); err != nil {
		return d, err
	}
	b.log("Local dependency diagnostics passed; EJS presence is not proof of playback or solver compatibility. No media URL was supplied.")
	return d, nil
}
func setEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return append(out, key+"="+value)
}
