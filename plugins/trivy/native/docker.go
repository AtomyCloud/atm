package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

const dockerStateName = ".atm-trivy-docker.json"

var imageIDRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var daemonIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,255}$`)

type dockerRecord struct {
	Tag     string `json:"tag"`
	ImageID string `json:"image_id"`
}
type dockerState struct {
	Schema   int                     `json:"schema"`
	Plugin   string                  `json:"plugin"`
	Selected string                  `json:"selected"`
	DaemonID string                  `json:"daemon_id"`
	Images   map[string]dockerRecord `json:"images"`
}

func emptyDockerState() dockerState {
	return dockerState{Schema: 1, Plugin: "trivy", Images: map[string]dockerRecord{}}
}
func (b *backend) readDockerState() (dockerState, error) {
	state := emptyDockerState()
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		return state, nil
	} else if err != nil {
		return state, err
	}
	file := filepath.Join(b.root, dockerStateName)
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return state, fmt.Errorf("unsafe Docker ownership state")
	}
	f, err := os.Open(file)
	if err != nil {
		return state, err
	}
	defer f.Close()
	state = dockerState{}
	dec := json.NewDecoder(io.LimitReader(f, (64<<10)+1))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&state); err != nil {
		return state, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return state, fmt.Errorf("trailing Docker state data")
	}
	if state.Schema != 1 || state.Plugin != "trivy" || state.Images == nil || len(state.Images) > 100 {
		return state, fmt.Errorf("invalid Docker ownership state")
	}
	if len(state.Images) > 0 && !daemonIDRE.MatchString(state.DaemonID) {
		return state, fmt.Errorf("Docker ownership requires a valid daemon identity")
	}
	for v, rec := range state.Images {
		if !versionRE.MatchString(v) || rec.Tag != "aquasec/trivy:"+v || !imageIDRE.MatchString(rec.ImageID) {
			return state, fmt.Errorf("invalid Docker tag/image ID receipt")
		}
	}
	if state.Selected != "" {
		if _, ok := state.Images[state.Selected]; !ok {
			return state, fmt.Errorf("selected Docker tag has no receipt")
		}
	}
	return state, nil
}
func (b *backend) saveDockerState(ctx context.Context, before, after dockerState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := b.readDockerState()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, before) {
		return fmt.Errorf("Docker ownership state changed concurrently")
	}
	file := filepath.Join(b.root, dockerStateName)
	if len(after.Images) == 0 {
		err = os.Remove(file)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := json.Marshal(after)
	if err != nil {
		return err
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("Docker ownership state too large")
	}
	f, err := os.CreateTemp(b.root, ".atm-trivy-docker-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}
func copyDockerState(s dockerState) dockerState {
	c := s
	c.Images = map[string]dockerRecord{}
	for v, r := range s.Images {
		c.Images[v] = r
	}
	return c
}
func (b *backend) docker(ctx context.Context, args ...string) (string, error) {
	exe, err := exec.LookPath("docker")
	if err != nil {
		return "", fmt.Errorf("Docker CLI unavailable; install and authorize Docker separately")
	}
	timeout := 15 * time.Second
	if len(args) > 0 && args[0] == "pull" {
		timeout = 5 * time.Minute
	}
	return b.run(ctx, exe, args, timeout)
}
func (b *backend) dockerReady(ctx context.Context, state dockerState) (string, error) {
	out, err := b.docker(ctx, "info", "--format", "{{.ID}}")
	if err != nil {
		return "", fmt.Errorf("Docker daemon unavailable or access denied (no sudo or automatic daemon start): %w", err)
	}
	id := strings.TrimSpace(out)
	if !daemonIDRE.MatchString(id) {
		return "", fmt.Errorf("Docker daemon returned an invalid identity")
	}
	if len(state.Images) > 0 && id != state.DaemonID {
		return "", fmt.Errorf("Docker daemon identity changed; preserving all images and receipts")
	}
	return id, nil
}
func (b *backend) inspectImage(ctx context.Context, rec dockerRecord) error {
	out, err := b.docker(ctx, "image", "inspect", "--format", "{{.Id}}", rec.Tag)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != rec.ImageID {
		return fmt.Errorf("Docker tag %s changed image ID; preserving it", rec.Tag)
	}
	return nil
}
func (b *backend) installDocker(ctx context.Context, o options) error {
	before, err := b.readDockerState()
	if err != nil {
		return err
	}
	daemonID, err := b.dockerReady(ctx, before)
	if err != nil {
		return err
	}
	spec, err := b.resolve(ctx, o.version, false)
	if err != nil {
		return err
	}
	v := spec.Version
	tag := "aquasec/trivy:" + v
	if b.req.Operation == "update" && before.Selected != "" && newer(before.Selected, v) {
		return fmt.Errorf("Docker update would downgrade the selected version")
	}
	if rec, ok := before.Images[v]; ok {
		if err = b.inspectImage(ctx, rec); err != nil {
			return err
		}
	} else {
		if len(before.Images) >= 100 {
			return fmt.Errorf("too many owned Docker tags; remove one before installing")
		}
		out, err := b.docker(ctx, "image", "ls", "--quiet", "--no-trunc", "--filter", "reference="+tag)
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) != "" {
			return fmt.Errorf("Docker tag %s already exists without native ownership; preserving it without adoption", tag)
		}
	}
	if _, err = b.docker(ctx, "pull", tag); err != nil {
		return fmt.Errorf("Docker pull failed; external image changes may remain without ownership: %w", err)
	}
	out, err := b.docker(ctx, "image", "inspect", "--format", "{{.Id}}", tag)
	if err != nil {
		return fmt.Errorf("pulled image not recorded; inspect failed: %w", err)
	}
	id := strings.TrimSpace(out)
	if !imageIDRE.MatchString(id) {
		return fmt.Errorf("pulled image not recorded; invalid image ID")
	}
	after := copyDockerState(before)
	after.DaemonID = daemonID
	after.Selected = v
	after.Images[v] = dockerRecord{tag, id}
	if err = b.saveDockerState(ctx, before, after); err != nil {
		return fmt.Errorf("image pull may have succeeded but ownership was not recorded: %w", err)
	}
	b.log("Recorded native Docker tag " + tag + " (" + id + ")")
	b.usage(v)
	return nil
}
func (b *backend) removeDocker(ctx context.Context, v string) error {
	before, err := b.readDockerState()
	if err != nil {
		return err
	}
	rec, ok := before.Images[v]
	if !ok {
		return fmt.Errorf("no native ownership record for Docker version %s; legacy/unowned images preserved", v)
	}
	if _, err = b.dockerReady(ctx, before); err != nil {
		return err
	}
	if err = b.inspectImage(ctx, rec); err != nil {
		return err
	}
	if _, err = b.docker(ctx, "image", "rm", rec.Tag); err != nil {
		return fmt.Errorf("Docker refused removal; ownership retained: %w", err)
	}
	after := copyDockerState(before)
	delete(after.Images, v)
	if after.Selected == v {
		after.Selected = ""
	}
	if err = b.saveDockerState(ctx, before, after); err != nil {
		return fmt.Errorf("Docker tag removed but ownership state was not updated: %w", err)
	}
	b.log("Removed recorded Docker tag only: " + rec.Tag)
	return nil
}
func sortedDockerVersions(s dockerState) []string {
	vs := []string{}
	for v := range s.Images {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool { return newer(vs[i], vs[j]) })
	return vs
}
func (b *backend) uninstallAll(ctx context.Context) error {
	// Preflight binary ownership and profiles before any nontransactional Docker
	// removal. Revalidate again in each individual removal operation.
	if _, err := b.current(); err != nil {
		return err
	}
	vs, err := b.versionDirs(false)
	if err != nil {
		return err
	}
	for _, v := range vs {
		if b.hasReceipt(v) {
			if err = b.verifyOwned(ctx, v); err != nil {
				return err
			}
		}
	}
	if _, err = b.profilePlans(true); err != nil {
		return err
	}
	state, err := b.readDockerState()
	if err != nil {
		return err
	}
	if len(state.Images) > 0 {
		if _, err = b.dockerReady(ctx, state); err != nil {
			return err
		}
		for _, v := range sortedDockerVersions(state) {
			if err = b.inspectImage(ctx, state.Images[v]); err != nil {
				return err
			}
		}
		for _, v := range sortedDockerVersions(state) {
			if err = b.removeDocker(ctx, v); err != nil {
				return err
			}
		}
	}
	return b.uninstall(ctx)
}
