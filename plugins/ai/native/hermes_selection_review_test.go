//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Internal consistency alone must not authorize a replacement that appeared
// after this selection request verified its preparation.
func TestHermesSelectionPinsCoherentCompletion(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, name := range []string{"hermes", "hermes-acp"} {
			t.Run(arch+"/"+name, func(t *testing.T) {
				f := newHermesAgentFixture(t, arch)
				mustRun(t, f.fixture, "install", hermesAgentArgs()...)
				root := f.b.root(hermesAgent)
				dir := filepath.Join(root, "versions", hermesPMVersion)
				current := filepath.Join(root, "current.json")
				if err := os.Remove(current); err != nil {
					t.Fatal(err)
				}
				calls, commands := f.calls, len(f.commands)
				seen := false
				body := []byte("# coherently replaced launcher; never execute\n")
				relative := "source/.hermes/bin/" + name
				launcher := filepath.Join(dir, filepath.FromSlash(relative))
				f.b.checkpoint = func(point string) error {
					if point != "hermes-before-select" {
						return nil
					}
					seen = true
					writeFixture(t, launcher, body, 0755)
					var complete hermesCompletion
					if err := readJSON(filepath.Join(dir, "complete.json"), &complete); err != nil {
						t.Fatal(err)
					}
					found := false
					for i := range complete.Evidence.Files {
						entry := &complete.Evidence.Files[i]
						if entry.Path == relative {
							entry.SHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
							entry.Size = int64(len(body))
							found = true
						}
					}
					if !found {
						t.Fatal("launcher missing from preparation evidence")
					}
					data, err := json.Marshal(complete)
					if err != nil {
						t.Fatal(err)
					}
					writeFixture(t, filepath.Join(dir, "complete.json"), data, 0600)
					if err := verifyHermesCompletion(context.Background(), dir, complete.Evidence); err != nil {
						t.Fatal("fixture must remain internally consistent", err)
					}
					return nil
				}
				if err := f.run(context.Background(), "use", "--provider", hermesAgent, "--version", hermesPMVersion); err == nil {
					t.Fatal("coherently replaced preparation was selected")
				}
				if !seen {
					t.Fatal("publication boundary not reached")
				}
				integrationMissing(t, current)
				if got := string(integrationRead(t, launcher)); got != string(body) {
					t.Fatal("modified launcher was overwritten")
				}
				if f.calls != calls || len(f.commands) != commands {
					t.Fatal("selection contacted network or executed PM")
				}
			})
		}
	}
}
