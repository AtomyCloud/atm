//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesLaunchRejectsCoherentReplacement(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			f := newHermesSelectionFixture(t, arch)
			root := f.b.root(hermesAgent)
			dir := filepath.Join(root, "versions", hermesPMVersion)
			completePath := filepath.Join(dir, "complete.json")
			var complete hermesCompletion
			if err := readJSON(completePath, &complete); err != nil {
				t.Fatal(err)
			}
			const secret = "synthetic-private-argument-do-not-journal"
			args := []string{"chat", secret}
			helperCalls, interactiveCalls := 0, 0
			f.b.hermes.run = func(context.Context, string, []string, string, []string) (string, error) {
				helperCalls++
				data, err := json.Marshal(append([]string{complete.Evidence.StorePython, "-I", "-c", "# trusted fixture", "--cli"}, args...))
				return string(data), err
			}
			f.b.hermes.interactive = func(context.Context, string, []string, string, []string) (hermesLaunchResult, error) {
				interactiveCalls++
				return hermesLaunchResult{}, nil
			}
			seen := false
			f.b.checkpoint = func(point string) error {
				if point != "hermes-after-launch-helper" {
					return nil
				}
				seen = true
				relative := "source/.hermes/bin/hermes"
				body := []byte("# coherent changed launch payload\n")
				writeFixture(t, filepath.Join(dir, filepath.FromSlash(relative)), body, 0755)
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
					t.Fatal("missing fixture launcher evidence")
				}
				data, err := json.Marshal(complete)
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, completePath, data, 0600)
				if err := verifyHermesCompletion(context.Background(), dir, complete.Evidence); err != nil {
					t.Fatal("fixture must remain internally consistent", err)
				}
				return nil
			}
			_, err := f.b.launchHermes(context.Background(), args)
			if err == nil || !seen || helperCalls != 1 || interactiveCalls != 0 {
				t.Fatal("changed preparation reached runtime", err, seen, helperCalls, interactiveCalls)
			}
			journal := integrationRead(t, filepath.Join(root+".lock", "launch.json"))
			if strings.Contains(string(journal)+err.Error(), secret) {
				t.Fatal("launch recovery leaked argument")
			}
			if f.calls != 0 {
				t.Fatal("launch contacted network")
			}
		})
	}
}
