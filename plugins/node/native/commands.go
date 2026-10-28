package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type commandRunner func(context.Context, string, []string, time.Duration) (string, error)

func (b *backend) probe(ctx context.Context, p string) string {
	if p == "" || !filepath.IsAbs(p) {
		return "unavailable"
	}
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "unavailable"
	}
	output, err := b.run(ctx, p, []string{"--version"}, 3*time.Second)
	if err != nil {
		return "unavailable"
	}
	v, err := normalizeVersion(strings.TrimSpace(output))
	if err != nil {
		return "unavailable"
	}
	return v
}
func (b *backend) status(ctx context.Context) error {
	vs, err := b.installed()
	if err != nil {
		return err
	}
	current, err := b.current()
	if err != nil {
		return err
	}
	if current == "" {
		b.log("ATM: not installed/selected")
	} else {
		b.log("ATM current: " + current + " (" + filepath.Join(b.root, "current") + ")")
	}
	for _, v := range vs {
		origin := "legacy/unowned (preserved)"
		if b.hasReceipt(v) {
			origin = "native receipt"
		}
		b.log("ATM version: " + v + " - " + origin)
	}
	local := os.Getenv("ATM_NODE_LOCAL_BINARY")
	if local == "" {
		local = filepath.Join(b.home, ".local/bin/node")
	}
	system := os.Getenv("ATM_NODE_SYSTEM_BINARY")
	if system == "" {
		system = "/usr/bin/node"
	}
	selected, _ := exec.LookPath("node")
	if selected != "" && !filepath.IsAbs(selected) {
		selected, _ = filepath.Abs(selected)
	}
	for _, p := range []struct{ label, path string }{{"User local", local}, {"System", system}, {"PATH", selected}} {
		if err = ctx.Err(); err != nil {
			return err
		}
		b.log(p.label + ": " + p.path + " - " + b.probe(ctx, p.path))
	}
	b.log("External executables are trusted local programs, not adopted or removed. Shell aliases may differ.")
	return ctx.Err()
}
func (b *backend) installSystem(ctx context.Context, o options) error {
	if !o.accept {
		return fmt.Errorf("system packages require CORE confirmation and --accept-system-packages; authorize sudo in a terminal with sudo -v first")
	}
	if _, err := os.Stat("/etc/debian_version"); err != nil {
		return fmt.Errorf("system installation requires Debian-family Linux with apt-get")
	}
	apt, err := exec.LookPath("apt-get")
	if err != nil {
		return fmt.Errorf("apt-get unavailable; only Debian-family system installation is supported")
	}
	args := []string{"--no-remove", "--yes", "install", "nodejs", "npm"}
	command := apt
	if os.Geteuid() != 0 {
		sudo, err := exec.LookPath("sudo")
		if err != nil {
			return fmt.Errorf("sudo unavailable; run sudo -v in a terminal before retrying")
		}
		command = sudo
		args = append([]string{"-n", "--", apt}, args...)
	}
	b.log("APT may install/upgrade dependencies; package changes are not transactional. No repository edits or full upgrades.")
	updateArgs := []string{"update"}
	if command != apt {
		updateArgs = []string{"-n", "--", apt, "update"}
	}
	_, err = b.run(ctx, command, updateArgs, 15*time.Minute)
	if err == nil {
		_, err = b.run(ctx, command, args, 15*time.Minute)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("APT failed: %w; for sudo authorization run sudo -v in a terminal, then retry (no password is read by ATM)", err)
	}
	node := os.Getenv("ATM_NODE_SYSTEM_BINARY")
	if node == "" {
		node = "/usr/bin/node"
	}
	npm := os.Getenv("ATM_NODE_SYSTEM_NPM")
	if npm == "" {
		npm = "/usr/bin/npm"
	}
	version := b.probe(ctx, node)
	info, statErr := os.Stat(npm)
	if version == "unavailable" || statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("APT completed but system Node/npm verification failed; inspect system packages manually")
	}
	b.log("APT verified Node.js " + version + " and npm executable. External packages remain system-owned. Portable current and profiles are unchanged.")
	return nil
}
