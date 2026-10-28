package main

import (
	"context"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dependencies struct {
	Node       string `json:"node"`
	NPM        string `json:"npm"`
	PS         string `json:"ps"`
	Version    string `json:"version"`
	NodeSHA256 string `json:"node_sha256"`
}
type commandRunner func(context.Context, string, []string, []string, string, time.Duration) (string, error)
type boundedOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := (64 << 10) - len(b.data)
	if len(p) > room {
		b.overflow = true
		p = p[:room]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func runCommand(ctx context.Context, path string, args, env []string, dir string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.WaitDelay = time.Second
	// Keep every child in CORE's process group, including probes and npm.
	out, stderr := &boundedOutput{}, &boundedOutput{}
	cmd.Stdout = out
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if out.overflow || stderr.overflow {
		return "", fmt.Errorf("dependency output exceeds 64 KiB")
	}
	if err != nil {
		return "", fmt.Errorf("trusted process %s failed: %w", filepath.Base(path), err)
	}
	return string(out.data), nil
}
func nativeExecutable(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute executable path required")
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if cleanPath(p) != nil || !executable(p) {
		return "", fmt.Errorf("regular executable required")
	}
	f, err := elf.Open(p)
	if err != nil {
		return "", fmt.Errorf("dependency must be a native ELF executable: %s", p)
	}
	defer f.Close()
	want := elf.EM_X86_64
	if runtime.GOARCH == "arm64" {
		want = elf.EM_AARCH64
	}
	if f.Machine != want || f.Class != elf.ELFCLASS64 {
		return "", fmt.Errorf("dependency ELF architecture mismatch")
	}
	return p, nil
}
func nodeSupported(v string) bool {
	v = strings.TrimPrefix(v, "v")
	if !stable.MatchString(v) {
		return false
	}
	p := strings.Split(v, ".")
	a, _ := strconv.Atoi(p[0])
	m, _ := strconv.Atoi(p[1])
	n, _ := strconv.Atoi(p[2])
	return a == 22 && (m > 23 || m == 23 && n >= 4) || a == 24 && (m > 22 || m == 22 && n >= 1) || a == 26 && (m > 12 || m == 12 && n >= 1) || a > 26
}
func cleanSearchPath(p string) string {
	var out []string
	for _, s := range filepath.SplitList(p) {
		if filepath.IsAbs(s) && !strings.ContainsAny(s, "\x00\r\n") {
			out = append(out, s)
		}
	}
	return strings.Join(out, ":")
}
func processEnv(home, node, ps string) []string {
	paths := []string{}
	if node != "" {
		paths = append(paths, filepath.Dir(node))
	}
	if ps != "" {
		paths = append(paths, filepath.Dir(ps))
	}
	paths = append(paths, os.Getenv("PATH"))
	return []string{"HOME=" + home, "TMPDIR=" + home, "PATH=" + cleanSearchPath(strings.Join(paths, ":")), "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "NO_COLOR=1"}
}
func (d dependencies) validate() error {
	for _, p := range []string{d.Node, d.NPM, d.PS} {
		if cleanPath(p) != nil {
			return fmt.Errorf("invalid recorded dependency path")
		}
	}
	if !nodeSupported(d.Version) || len(d.NodeSHA256) != 64 {
		return fmt.Errorf("invalid runtime record")
	}
	return nil
}
func (b *backend) dependencies(ctx context.Context, work string) (dependencies, error) {
	var d dependencies
	ps, _ := exec.LookPath("ps")
	var err error
	d.PS, err = nativeExecutable(ps)
	if err != nil {
		return d, fmt.Errorf("procps-compatible native ps required on PATH; install procps/procps-ng separately: %w", err)
	}
	if within(d.PS, b.root) {
		return d, fmt.Errorf("REA payload cannot supply its own prerequisites")
	}
	out, err := b.run(ctx, d.PS, []string{"-p", strconv.Itoa(os.Getpid()), "-o", "pid=", "-o", "ppid=", "-o", "stat=", "-o", "args="}, processEnv(work, "", d.PS), work, 5*time.Second)
	if err != nil {
		return d, err
	}
	fields := strings.Fields(out)
	if len(fields) < 4 || fields[0] != strconv.Itoa(os.Getpid()) {
		return d, fmt.Errorf("ps lacks required procps output")
	}
	if _, err = strconv.Atoi(fields[1]); err != nil {
		return d, fmt.Errorf("ps returned invalid parent PID")
	}
	pathNode, _ := exec.LookPath("node")
	seen := map[string]bool{}
	for _, candidate := range []string{pathNode, filepath.Join(b.req.AppsDir, "node/current/bin/node")} {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if _, err = os.Stat(candidate); os.IsNotExist(err) {
			continue
		}
		node, e := nativeExecutable(candidate)
		if e != nil {
			continue
		}
		if within(node, b.root) {
			continue
		}
		output, e := b.run(ctx, node, []string{"--version"}, processEnv(work, node, d.PS), work, 5*time.Second)
		if e != nil {
			return d, e
		}
		v := strings.TrimPrefix(strings.TrimSpace(output), "v")
		if !nodeSupported(v) {
			continue
		}
		npm := filepath.Join(filepath.Dir(node), "npm")
		if _, e = os.Stat(npm); os.IsNotExist(e) {
			npm, _ = exec.LookPath("npm")
		}
		if !filepath.IsAbs(npm) {
			continue
		}
		npm, e = filepath.EvalSymlinks(npm)
		if e != nil {
			continue
		}
		if filepath.Base(npm) != "npm-cli.js" || within(npm, b.root) {
			continue
		}
		if _, e = readRegular(npm, 16<<20); e != nil {
			continue
		}
		output, e = b.run(ctx, node, []string{npm, "--version"}, processEnv(work, node, d.PS), work, 10*time.Second)
		if e != nil {
			return d, e
		}
		if !stable.MatchString(strings.TrimSpace(output)) {
			continue
		}
		d.Node = node
		d.NPM = npm
		d.Version = v
		d.NodeSHA256, e = hashFile(ctx, node)
		if e != nil {
			return d, e
		}
		return d, d.validate()
	}
	return d, fmt.Errorf("compatible Node/npm unavailable: use native Node management separately (Node 22 >=22.23.4, 24 >=24.22.1, or >=26.12.1 stable)")
}
func (b *backend) diagnostics(ctx context.Context, trust bool) error {
	b.log("Required: Node 22 >=22.23.4, 24 >=24.22.1, or >=26.12.1 stable, paired npm and procps-compatible ps. Python/timeout are not native ATM helper dependencies.")
	b.log("No automatic repair: manage Node separately with ATM; install procps/procps-ng through your distribution. Optional providers/native bindings need separate preparation; --ignore-scripts may leave them unavailable.")
	if !trust {
		b.log("No dependency executable was run. --trust-dependencies authorizes local Node/npm/ps probes only, not package/system repair.")
		return nil
	}
	work, err := os.MkdirTemp("", "atm-rea-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	d, err := b.dependencies(ctx, work)
	if err != nil {
		return err
	}
	b.log("Trusted diagnostics passed: Node " + d.Version + " at " + d.Node + "; npm " + d.NPM + "; ps " + d.PS + ". This is not REA/provider readiness.")
	return nil
}
