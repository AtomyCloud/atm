package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
)

type artifact struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	Integrity string `json:"integrity"`
	Engine    string `json:"engine"`
}

func (a artifact) validate() error {
	if !stable.MatchString(a.Version) || a.URL != registry+"/rea-agents/-/rea-agents-"+a.Version+".tgz" || a.Engine == "" || len(a.Engine) > 512 {
		return fmt.Errorf("invalid official REA artifact")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(a.Integrity, "sha512-"))
	if err != nil || !strings.HasPrefix(a.Integrity, "sha512-") || len(raw) != 64 {
		return fmt.Errorf("official SHA-512 integrity required")
	}
	return nil
}
func (b *backend) get(ctx context.Context, location string) (io.ReadCloser, error) {
	allowed := func(u *url.URL) bool {
		return u.Scheme == "https" && u.Host == "registry.npmjs.org" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
	}
	u, err := url.Parse(location)
	if err != nil || !allowed(u) {
		return nil, fmt.Errorf("only official HTTPS npm registry accepted")
	}
	client := *b.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !allowed(req.URL) {
			return fmt.Errorf("registry redirect refused")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("official registry HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}
func (b *backend) resolve(ctx context.Context, version string) (artifact, error) {
	var a artifact
	if version != "latest" && !stable.MatchString(version) {
		return a, fmt.Errorf("invalid version")
	}
	body, err := b.get(ctx, registry+"/rea-agents/"+version)
	if err != nil {
		return a, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, (4<<20)+1))
	if err != nil {
		return a, err
	}
	if len(data) > 4<<20 {
		return a, fmt.Errorf("oversized registry metadata")
	}
	var m struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
		Bin  map[string]string `json:"bin"`
		Dist struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	if err = decodeJSON(data, &m, false); err != nil {
		return a, err
	}
	if m.Name != "rea-agents" || m.Bin["rea"] != "scripts/rea.mjs" || (version != "latest" && version != m.Version) {
		return a, fmt.Errorf("official package identity/entrypoint mismatch")
	}
	a = artifact{m.Version, m.Dist.Tarball, m.Dist.Integrity, m.Engines.Node}
	return a, a.validate()
}
func (b *backend) download(ctx context.Context, a artifact, dest string) error {
	body, err := b.get(ctx, a.URL)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha512.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, body}, (128<<20)+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n > 128<<20 {
		return fmt.Errorf("REA archive exceeds 128 MiB")
	}
	if "sha512-"+base64.StdEncoding.EncodeToString(h.Sum(nil)) != a.Integrity {
		return fmt.Errorf("REA archive integrity mismatch")
	}
	return inspectArchive(ctx, dest, a)
}
func registrySpec(s string) bool {
	if strings.ContainsAny(s, "/\\:\x00\r\n") || strings.HasPrefix(s, ".") {
		return false
	}
	return s != ""
}
func inspectArchive(ctx context.Context, file string, a artifact) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	r := tar.NewReader(io.LimitReader(contextReader{ctx, gz}, maxTotal+1))
	seen := map[string]bool{}
	packageSeen := false
	var packageDeps map[string]string
	var lockData any
	var total int64
	for n := 0; ; n++ {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		name := strings.TrimSuffix(h.Name, "/")
		if n >= 100000 || path.Clean(name) != name || !strings.HasPrefix(name, "package/") || strings.ContainsAny(name, "\\\x00\r\n") || seen[name] {
			return fmt.Errorf("unsafe/duplicate archive path")
		}
		seen[name] = true
		if h.Mode&07000 != 0 || h.Size < 0 || h.Size > maxFile || h.Size > maxTotal-total {
			return fmt.Errorf("unsafe archive mode/size")
		}
		total += h.Size
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
		default:
			return fmt.Errorf("REA top-level archive links/special files refused")
		}
		if name != "package/package.json" && name != "package/npm-shrinkwrap.json" {
			continue
		}
		if h.Size > 16<<20 {
			return fmt.Errorf("oversized npm manifest")
		}
		data, e := io.ReadAll(r)
		if e != nil {
			return e
		}
		if name == "package/package.json" {
			var m struct {
				Name    string `json:"name"`
				Version string `json:"version"`
				Engines struct {
					Node string `json:"node"`
				} `json:"engines"`
				Bin          map[string]string `json:"bin"`
				Dependencies map[string]string `json:"dependencies"`
				Optional     map[string]string `json:"optionalDependencies"`
				Peers        map[string]string `json:"peerDependencies"`
				Bundled      any               `json:"bundledDependencies"`
				Bundle       any               `json:"bundleDependencies"`
			}
			if e = decodeJSON(data, &m, false); e != nil {
				return e
			}
			if m.Name != "rea-agents" || m.Version != a.Version || m.Engines.Node != a.Engine || m.Bin["rea"] != "scripts/rea.mjs" {
				return fmt.Errorf("archive package identity mismatch")
			}
			if m.Bundled != nil || m.Bundle != nil {
				return fmt.Errorf("bundled dependencies are unsupported")
			}
			packageDeps = map[string]string{}
			for _, deps := range []map[string]string{m.Dependencies, m.Optional, m.Peers} {
				for name, v := range deps {
					if !registrySpec(v) || !packageName.MatchString(name) {
						return fmt.Errorf("non-registry direct dependency refused")
					}
					packageDeps[name] = v
				}
			}
			packageSeen = true
		} else {
			if e = decodeJSON(data, &lockData, false); e != nil {
				return e
			}
			if e = checkLock(lockData); e != nil {
				return e
			}
		}
	}
	if !packageSeen {
		return fmt.Errorf("missing archive package.json")
	}
	if len(packageDeps) > 0 && lockData != nil {
		lock := lockData.(map[string]any)
		packages := lock["packages"].(map[string]any)
		for name := range packageDeps {
			if _, ok := packages["node_modules/"+name]; !ok {
				return fmt.Errorf("shrinkwrap missing direct dependency %s", name)
			}
		}
	}
	return nil
}

var packageName = regexp.MustCompile(`^(?:@[a-zA-Z0-9._-]+/)?[a-zA-Z0-9._-]+$`)
var packageVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][a-zA-Z0-9.+-]+)?$`)

func checkLock(v any) error {
	root, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("shrinkwrap object required")
	}
	lv, ok := root["lockfileVersion"].(float64)
	if !ok || (lv != 2 && lv != 3) {
		return fmt.Errorf("only complete npm shrinkwrap v2/v3 supported")
	}
	packages, ok := root["packages"].(map[string]any)
	if !ok || len(packages) == 0 {
		return fmt.Errorf("missing shrinkwrap packages graph")
	}
	if _, ok = packages[""]; !ok {
		return fmt.Errorf("missing shrinkwrap root")
	}
	if err := checkLockValues(v); err != nil {
		return err
	}
	for location, raw := range packages {
		p, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid shrinkwrap package")
		}
		if location != "" {
			if path.Clean(location) != location || !strings.HasPrefix(location, "node_modules/") || strings.ContainsAny(location, "\\\x00\r\n") {
				return fmt.Errorf("unsafe shrinkwrap location")
			}
			version, ok := p["version"].(string)
			if !ok || !packageVersion.MatchString(version) {
				return fmt.Errorf("non-registry shrinkwrap version")
			}
			resolved, ok := p["resolved"].(string)
			if !ok || !officialResolution(resolved) {
				return fmt.Errorf("missing/non-registry shrinkwrap resolution")
			}
			integrity, ok := p["integrity"].(string)
			sum, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(integrity, "sha512-"))
			if !ok || e != nil || !strings.HasPrefix(integrity, "sha512-") || len(sum) != 64 {
				return fmt.Errorf("shrinkwrap SHA-512 integrity required")
			}
		}
		for _, field := range []string{"dependencies", "optionalDependencies", "peerDependencies"} {
			deps, exists := p[field]
			if !exists {
				continue
			}
			m, ok := deps.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid dependency map")
			}
			for name, spec := range m {
				value, ok := spec.(string)
				if !ok || !packageName.MatchString(name) || !registrySpec(value) {
					return fmt.Errorf("unsupported shrinkwrap dependency acquisition")
				}
				base := location
				found := false
				for {
					if _, ok := packages[path.Join(base, "node_modules", name)]; ok {
						found = true
						break
					}
					if base == "" {
						break
					}
					idx := strings.LastIndex(base, "node_modules/")
					if idx < 0 {
						base = ""
					} else {
						base = strings.TrimSuffix(base[:idx], "/")
					}
				}
				if !found {
					return fmt.Errorf("incomplete shrinkwrap graph: %s requires %s", location, name)
				}
			}
		}
	}
	return nil
}
func officialResolution(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host == "registry.npmjs.org" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func checkLockValues(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "resolved" {
				s, ok := v.(string)
				if !ok || !officialResolution(s) {
					return fmt.Errorf("non-registry shrinkwrap resolution refused")
				}
			}
			if k == "version" {
				s, ok := v.(string)
				if !ok || !packageVersion.MatchString(s) {
					return fmt.Errorf("unsupported shrinkwrap version acquisition")
				}
			}
			if k == "link" && v == true {
				return fmt.Errorf("linked shrinkwrap dependency refused")
			}
			if err := checkLockValues(v); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range x {
			if err := checkLockValues(v); err != nil {
				return err
			}
		}
	}
	return nil
}
func validatePackage(dir, version string) error {
	data, err := readRegular(dir+"/lib/node_modules/rea-agents/package.json", 4<<20)
	if err != nil {
		return err
	}
	var p struct {
		Name    string            `json:"name"`
		Version string            `json:"version"`
		Bin     map[string]string `json:"bin"`
	}
	if err = decodeJSON(data, &p, false); err != nil {
		return err
	}
	if p.Name != "rea-agents" || p.Version != version || p.Bin["rea"] != "scripts/rea.mjs" {
		return fmt.Errorf("installed package identity mismatch")
	}
	data, err = readRegular(dir+"/lib/node_modules/rea-agents/scripts/rea.mjs", 16<<20)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty REA entrypoint")
	}
	return nil
}
