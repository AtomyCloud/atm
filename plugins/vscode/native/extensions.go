package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var extensionRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}\.[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)

func parseExtensions(data []byte) ([]string, error) {
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("extension list too large")
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !extensionRE.MatchString(line) {
			return nil, fmt.Errorf("invalid literal publisher.extension identifier")
		}
		key := strings.ToLower(line)
		if !seen[key] {
			seen[key] = true
			ids = append(ids, line)
		}
		if len(ids) > 1000 {
			return nil, fmt.Errorf("too many extensions")
		}
	}
	return ids, nil
}
func (b *backend) installExtensions(ctx context.Context, v string) error {
	// Only a verified native-owned payload may be executed. No shell launcher,
	// remote CLI hook, NODE_OPTIONS or Electron injection environment is inherited.
	if err := b.verifyOwned(ctx, v); err != nil {
		return err
	}
	directory := filepath.Join(b.req.Home, "plugins/vscode")
	if err := realDirs(directory, false); err != nil {
		return err
	}
	data, _, exists, err := readProfile(filepath.Join(directory, "extensions.txt"))
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("extensions.txt is missing")
	}
	ids, err := parseExtensions(data)
	if err != nil {
		return err
	}
	dest := filepath.Join(b.root, v)
	if err = realDirs(filepath.Join(dest, "data"), false); err != nil {
		return err
	}
	// Inspect data before executing to reject portable paths escaping through links.
	if _, err = dataSnapshot(ctx, filepath.Join(dest, "data")); err != nil {
		return err
	}
	env := []string{"HOME=" + b.home, "ELECTRON_RUN_AS_NODE=1", "VSCODE_PORTABLE=" + filepath.Join(dest, "data")}
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return err
		}
		b.log("Installing extension " + id)
		commandCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err = b.run(commandCtx, filepath.Join(dest, "code"), []string{filepath.Join(dest, "resources/app/out/cli.js"), "--install-extension", id, "--force"}, env)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("extension %s failed: %w; earlier extensions may already be installed", id, err)
		}
	}
	b.log("Extensions completed; portable data is user-owned and preserved")
	return nil
}
