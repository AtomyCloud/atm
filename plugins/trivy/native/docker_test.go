//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The test executable doubles as an inert Docker CLI. It accepts only the
// literal argv forms used by this plugin and never starts a daemon/container.
func TestMain(m *testing.M) {
	if root := os.Getenv("ATM_TRIVY_TEST_DOCKER"); root != "" {
		os.Exit(fakeDocker(root, os.Args[1:]))
	}
	os.Exit(m.Run())
}
func fakeDocker(root string, args []string) int {
	log, err := os.OpenFile(filepath.Join(root, "calls.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return 90
	}
	_ = json.NewEncoder(log).Encode(args)
	log.Close()
	fail := os.Getenv("ATM_TRIVY_TEST_FAIL")
	if fail == "wait" {
		time.Sleep(time.Minute)
		return 0
	}
	if fail == "noise" {
		fmt.Print(strings.Repeat("x", 20000))
		return 0
	}
	images := map[string]string{}
	if data, err := os.ReadFile(filepath.Join(root, "images.json")); err == nil {
		if json.Unmarshal(data, &images) != nil {
			return 91
		}
	}
	save := func() int {
		data, _ := json.Marshal(images)
		if os.WriteFile(filepath.Join(root, "images.json"), data, 0600) != nil {
			return 92
		}
		return 0
	}
	if reflect.DeepEqual(args, []string{"info", "--format", "{{.ID}}"}) {
		if fail == "info" {
			return 1
		}
		id := os.Getenv("ATM_TRIVY_TEST_DAEMON")
		if id == "" {
			id = "daemon-A"
		}
		fmt.Println(id)
		return 0
	}
	if len(args) == 6 && reflect.DeepEqual(args[:5], []string{"image", "ls", "--quiet", "--no-trunc", "--filter"}) && strings.HasPrefix(args[5], "reference=aquasec/trivy:") {
		if fail == "ls" {
			return 1
		}
		if id := images[strings.TrimPrefix(args[5], "reference=")]; id != "" {
			fmt.Println(id)
		}
		return 0
	}
	if len(args) == 2 && args[0] == "pull" && strings.HasPrefix(args[1], "aquasec/trivy:") {
		if fail == "pull" {
			return 1
		}
		images[args[1]] = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(args[1])))
		if code := save(); code != 0 {
			return code
		}
		if fail == "pull-wait" {
			_ = os.WriteFile(filepath.Join(root, "pulled"), []byte("done"), 0600)
			time.Sleep(time.Minute)
		}
		fmt.Println("pulled")
		return 0
	}
	if len(args) == 5 && reflect.DeepEqual(args[:4], []string{"image", "inspect", "--format", "{{.Id}}"}) && strings.HasPrefix(args[4], "aquasec/trivy:") {
		if fail == "inspect" {
			return 1
		}
		if fail == "bad-id" {
			fmt.Println("invalid")
			return 0
		}
		if id := images[args[4]]; id != "" {
			fmt.Println(id)
			return 0
		}
		return 1
	}
	if len(args) == 3 && args[0] == "image" && args[1] == "rm" && strings.HasPrefix(args[2], "aquasec/trivy:") {
		if fail == "rm" {
			return 1
		}
		if _, ok := images[args[2]]; !ok {
			return 1
		}
		delete(images, args[2])
		return save()
	}
	fmt.Fprintln(os.Stderr, "unexpected Docker fixture argv", args)
	return 93
}
func dockerFixture(t *testing.T, b *backend) string {
	t.Helper()
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(exe, filepath.Join(root, "docker")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("ATM_TRIVY_TEST_DOCKER", root)
	t.Setenv("ATM_TRIVY_TEST_DAEMON", "daemon-A")
	t.Setenv("ATM_TRIVY_TEST_FAIL", "")
	b.run = runCommand
	return root
}
func dockerCalls(t *testing.T, root string) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "calls.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	calls := [][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var args []string
		if err = json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, args)
	}
	return calls
}
func dockerImages(t *testing.T, root string) map[string]string {
	t.Helper()
	images := map[string]string{}
	data, err := os.ReadFile(filepath.Join(root, "images.json"))
	if os.IsNotExist(err) {
		return images
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &images); err != nil {
		t.Fatal(err)
	}
	return images
}
func setImages(t *testing.T, root string, images map[string]string) {
	t.Helper()
	data, _ := json.Marshal(images)
	write(t, filepath.Join(root, "images.json"), data, 0600)
}
func forbidMutations(t *testing.T, calls [][]string) {
	t.Helper()
	for _, args := range calls {
		if len(args) > 0 && (args[0] == "pull" || (len(args) > 1 && args[0] == "image" && args[1] == "rm")) {
			t.Fatal("unexpected mutation", args)
		}
	}
}

func TestDockerLifecycleLiteralArgvAndPreservation(t *testing.T) {
	b := newTestBackend(t)
	root := dockerFixture(t, b)
	releasesFixture(t, b, "0.76.1", "0.75.0")
	externalID := "sha256:" + strings.Repeat("e", 64)
	setImages(t, root, map[string]string{"other/image:keep": externalID, "aquasec/trivy:0.74.0": externalID})
	legacyState := filepath.Join(b.req.Home, "state/trivy/docker-version")
	write(t, legacyState, []byte("0.74.0\n"), 0600)
	if err := action(b, "install", "--method", "docker"); err != nil {
		t.Fatal(err)
	}
	calls := dockerCalls(t, root)
	want := [][]string{{"info", "--format", "{{.ID}}"}, {"image", "ls", "--quiet", "--no-trunc", "--filter", "reference=aquasec/trivy:0.76.1"}, {"pull", "aquasec/trivy:0.76.1"}, {"image", "inspect", "--format", "{{.Id}}", "aquasec/trivy:0.76.1"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatal(calls)
	}
	state, err := b.readDockerState()
	if err != nil || state.DaemonID != "daemon-A" || state.Selected != "0.76.1" || len(state.Images) != 1 {
		t.Fatal(state, err)
	}
	before := len(calls)
	if err = action(b, "status"); err != nil {
		t.Fatal(err)
	}
	if err = action(b, "menu"); err != nil {
		t.Fatal(err)
	}
	if len(dockerCalls(t, root)) != before {
		t.Fatal("status/menu invoked Docker")
	}
	if err = action(b, "install", "--method", "docker", "--version", "0.75.0"); err != nil {
		t.Fatal(err)
	}
	if err = action(b, "update", "--method", "docker"); err != nil {
		t.Fatal(err)
	}
	if err = action(b, "remove", "--method", "docker", "--version", "0.75.0"); err != nil {
		t.Fatal(err)
	}
	if err = action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	images := dockerImages(t, root)
	if !reflect.DeepEqual(images, map[string]string{"other/image:keep": externalID, "aquasec/trivy:0.74.0": externalID}) {
		t.Fatal("unrelated image changed", images)
	}
	data, _ := os.ReadFile(legacyState)
	if string(data) != "0.74.0\n" {
		t.Fatal("legacy state changed")
	}
	assertAbsent(t, filepath.Join(b.root, dockerStateName))
	assertAbsent(t, filepath.Join(b.home, ".profile"))
	assertClean(t, b)
}

func TestDockerExistingTagNeverAdopted(t *testing.T) {
	b := newTestBackend(t)
	root := dockerFixture(t, b)
	releasesFixture(t, b, "0.76.1")
	id := "sha256:" + strings.Repeat("a", 64)
	setImages(t, root, map[string]string{"aquasec/trivy:0.76.1": id})
	write(t, filepath.Join(b.req.Home, "state/trivy/docker-version"), []byte("0.76.1"), 0600)
	for _, op := range []string{"install", "update"} {
		if err := action(b, op, "--method", "docker"); err == nil {
			t.Fatal("adopted existing tag")
		}
	}
	if err := action(b, "remove", "--method", "docker", "--version", "0.76.1"); err == nil {
		t.Fatal("removed unowned image")
	}
	if err := action(b, "uninstall"); err != nil {
		t.Fatal(err)
	}
	forbidMutations(t, dockerCalls(t, root))
	assertAbsent(t, filepath.Join(b.root, dockerStateName))
	if dockerImages(t, root)["aquasec/trivy:0.76.1"] != id {
		t.Fatal("external image changed")
	}
}

func TestDockerDaemonOrTagChangeRefusesAllMutation(t *testing.T) {
	for _, kind := range []string{"daemon", "image"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			root := dockerFixture(t, b)
			releasesFixture(t, b, "0.76.1")
			if err := action(b, "install", "--method", "docker"); err != nil {
				t.Fatal(err)
			}
			receiptBefore, err := os.ReadFile(filepath.Join(b.root, dockerStateName))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "daemon" {
				t.Setenv("ATM_TRIVY_TEST_DAEMON", "daemon-B")
			} else {
				setImages(t, root, map[string]string{"aquasec/trivy:0.76.1": "sha256:" + strings.Repeat("f", 64)})
			}
			first := len(dockerCalls(t, root))
			for _, op := range []string{"install", "update"} {
				if err := action(b, op, "--method", "docker"); err == nil {
					t.Fatal("changed ownership accepted", kind, op)
				}
			}
			if err := action(b, "remove", "--method", "docker", "--version", "0.76.1"); err == nil {
				t.Fatal("changed ownership removal")
			}
			if err := action(b, "uninstall"); err == nil {
				t.Fatal("changed ownership uninstall")
			}
			forbidMutations(t, dockerCalls(t, root)[first:])
			after, _ := os.ReadFile(filepath.Join(b.root, dockerStateName))
			if string(after) != string(receiptBefore) {
				t.Fatal("receipt changed")
			}
		})
	}
}

func TestDockerFailureDoesNotInventOwnership(t *testing.T) {
	for _, failure := range []string{"info", "ls", "pull", "inspect", "bad-id"} {
		t.Run(failure, func(t *testing.T) {
			b := newTestBackend(t)
			root := dockerFixture(t, b)
			releasesFixture(t, b, "0.76.1")
			t.Setenv("ATM_TRIVY_TEST_FAIL", failure)
			if err := action(b, "install", "--method", "docker"); err == nil {
				t.Fatal("failure accepted")
			}
			assertAbsent(t, filepath.Join(b.root, dockerStateName))
			assertClean(t, b)
			if failure == "inspect" || failure == "bad-id" {
				if dockerImages(t, root)["aquasec/trivy:0.76.1"] == "" {
					t.Fatal("fixture pull did not happen")
				}
			}
		})
	}
	b := newTestBackend(t)
	root := dockerFixture(t, b)
	releasesFixture(t, b, "0.76.1")
	t.Setenv("ATM_TRIVY_TEST_DAEMON", "not a valid daemon ID")
	if err := action(b, "install", "--method", "docker"); err == nil {
		t.Fatal("invalid daemon accepted")
	}
	forbidMutations(t, dockerCalls(t, root))
}

func TestDockerRemovalFailurePreservesBinaryAndReceipt(t *testing.T) {
	b := newTestBackend(t)
	root := dockerFixture(t, b)
	releasesFixture(t, b, "0.76.1")
	if err := action(b, "install", "--method", "binary"); err != nil {
		t.Fatal(err)
	}
	if err := action(b, "install", "--method", "docker"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(b.root, dockerStateName))
	t.Setenv("ATM_TRIVY_TEST_FAIL", "rm")
	if err := action(b, "uninstall"); err == nil {
		t.Fatal("failed removal succeeded")
	}
	assertCurrent(t, b, "0.76.1")
	after, _ := os.ReadFile(filepath.Join(b.root, dockerStateName))
	if string(before) != string(after) {
		t.Fatal("failed removal changed receipt")
	}
	t.Setenv("ATM_TRIVY_TEST_FAIL", "")
	write(t, filepath.Join(b.root, "0.76.1/user-data"), []byte("mine"), 0600)
	first := len(dockerCalls(t, root))
	if err := action(b, "uninstall"); err == nil {
		t.Fatal("modified binary accepted")
	}
	forbidMutations(t, dockerCalls(t, root)[first:])
}

func TestDockerCancellationAfterPullAndBoundedCommands(t *testing.T) {
	b := newTestBackend(t)
	root := dockerFixture(t, b)
	releasesFixture(t, b, "0.76.1")
	t.Setenv("ATM_TRIVY_TEST_FAIL", "pull-wait")
	b.req.Operation = "install"
	b.req.Arguments = []string{"--method", "docker"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := b.execute(ctx); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "pulled")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("fixture pull never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	if err := <-done; err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("unbounded cancellation", err)
	}
	assertAbsent(t, filepath.Join(b.root, dockerStateName))
	if dockerImages(t, root)["aquasec/trivy:0.76.1"] == "" {
		t.Fatal("expected nontransactional image residue")
	}
	assertClean(t, b)
	t.Setenv("ATM_TRIVY_TEST_FAIL", "noise")
	if _, err := runCommand(context.Background(), filepath.Join(root, "docker"), []string{"image", "inspect"}, time.Second); err == nil {
		t.Fatal("unbounded image output")
	}
	if out, err := runCommand(context.Background(), filepath.Join(root, "docker"), []string{"pull", "aquasec/trivy:0.76.1"}, time.Second); err != nil || len(out) > 8192 {
		t.Fatal("pull output not bounded", err)
	}
	t.Setenv("ATM_TRIVY_TEST_FAIL", "wait")
	start = time.Now()
	if _, err := runCommand(context.Background(), filepath.Join(root, "docker"), []string{"info"}, 50*time.Millisecond); err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("deadline ignored", err)
	}
}

func TestDockerReceiptSafetyAndDryRun(t *testing.T) {
	for _, kind := range []string{"symlink", "malformed", "missing-daemon"} {
		t.Run(kind, func(t *testing.T) {
			b := newTestBackend(t)
			root := dockerFixture(t, b)
			if err := os.MkdirAll(b.root, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(b.root, dockerStateName)
			switch kind {
			case "symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "external"), file); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				write(t, file, []byte("{}"), 0600)
			case "missing-daemon":
				state := emptyDockerState()
				state.Selected = "0.76.1"
				state.Images["0.76.1"] = dockerRecord{"aquasec/trivy:0.76.1", "sha256:" + strings.Repeat("a", 64)}
				data, _ := json.Marshal(state)
				write(t, file, data, 0600)
			}
			if err := action(b, "install", "--method", "docker"); err == nil {
				t.Fatal("unsafe state accepted")
			}
			if len(dockerCalls(t, root)) != 0 {
				t.Fatal("unsafe state executed Docker")
			}
		})
	}
	b := newTestBackend(t)
	root := dockerFixture(t, b)
	t.Setenv("ATM_DRY_RUN", "1")
	for _, op := range []string{"install", "update", "remove", "uninstall"} {
		if err := action(b, op, "--method", "docker", "--version", "0.76.1"); err != nil {
			t.Fatal(err)
		}
	}
	assertAbsent(t, b.root)
	if len(dockerCalls(t, root)) != 0 {
		t.Fatal("dry run executed Docker")
	}
}
