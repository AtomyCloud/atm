//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AtomyCloud/atm/internal/install"
)

func TestHermesBootstrapCoordinatedMutationBeforeExecution(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, resize := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/resize=%t", arch, resize), func(t *testing.T) {
				f := newHermesAgentFixture(t, arch)
				ctx := context.Background()
				reached := false
				// Even an unfixed implementation must never execute fixture bytes
				// or proceed into unrelated Python/PM completion machinery.
				f.b.hermes.run = func(_ context.Context, executable string, args []string, _ string, _ []string) (string, error) {
					f.commands = append(f.commands, append([]string{executable}, args...))
					return "", fmt.Errorf("unexpected bootstrap execution after coordinated mutation")
				}
				f.b.checkpoint = func(point string) error {
					if point != "hermes-before-bootstrap" {
						return nil
					}
					if reached {
						t.Fatal("bootstrap boundary reached more than once")
					}
					reached = true
					versionDir := filepath.Join(f.b.root(hermesAgent), "versions", hermesPMVersion)
					pins, err := readHermesPins(filepath.Join(versionDir, "source"), arch)
					if err != nil {
						t.Fatal(err)
					}
					manager := install.Manager{Root: filepath.Join(versionDir, "bootstrap"), PluginID: "ai", Client: f.b.client}
					active, err := manager.Active(ctx)
					if err != nil || active.Version != pins.UVVersion || active.Generation < 1 {
						t.Fatalf("invalid initial Manager state: %+v, %v", active, err)
					}
					uvPath := filepath.Join(active.Path, pins.UVEntry)
					slotPath := filepath.Join(versionDir, "home", ".hermes", "tools", "uv-"+pins.UVVersion+"-"+pins.Target, "uv")
					original, err := os.ReadFile(uvPath)
					if err != nil || len(original) == 0 {
						t.Fatalf("read original UV: %v", err)
					}
					slot, err := os.ReadFile(slotPath)
					if err != nil || !bytes.Equal(original, slot) {
						t.Fatalf("initial PM slot differs: %v", err)
					}
					changed := append([]byte(nil), original...)
					changed[len(changed)-1] ^= 0x5a
					if resize {
						changed = append(changed, 0x7f)
					}
					for _, name := range []string{uvPath, slotPath} {
						if err := os.WriteFile(name, changed, 0700); err != nil {
							t.Fatal(err)
						}
					}

					var receipt struct {
						Schema   int                     `json:"schema"`
						PluginID string                  `json:"plugin_id"`
						Artifact install.Spec            `json:"artifact"`
						Files    map[string]install.File `json:"files"`
					}
					receiptPath := filepath.Join(manager.Root, "versions", pins.UVVersion, "receipt.json")
					if err := readJSON(receiptPath, &receipt); err != nil {
						t.Fatal(err)
					}
					if receipt.Artifact.URL != pins.UV.URL || receipt.Artifact.SHA256 != pins.UV.SHA256 {
						t.Fatal("initial receipt does not match source-pinned archive")
					}
					entry, ok := receipt.Files[pins.UVEntry]
					if !ok || entry.Size != int64(len(original)) || entry.SHA256 != fmt.Sprintf("%x", sha256.Sum256(original)) {
						t.Fatal("initial executable receipt differs")
					}
					entry.SHA256 = fmt.Sprintf("%x", sha256.Sum256(changed))
					entry.Size = int64(len(changed))
					receipt.Files[pins.UVEntry] = entry
					data, err := json.Marshal(receipt)
					if err != nil {
						t.Fatal(err)
					}
					data = append(data, '\n')
					if err := os.WriteFile(receiptPath, data, 0600); err != nil {
						t.Fatal(err)
					}

					var activation struct {
						Schema        int    `json:"schema"`
						Version       string `json:"version"`
						ReceiptSHA256 string `json:"receipt_sha256"`
						Generation    int    `json:"generation"`
					}
					activationPath := filepath.Join(manager.Root, "activations", fmt.Sprintf("%020d.json", active.Generation))
					if err := readJSON(activationPath, &activation); err != nil {
						t.Fatal(err)
					}
					activation.ReceiptSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
					data, err = json.Marshal(activation)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(activationPath, append(data, '\n'), 0600); err != nil {
						t.Fatal(err)
					}
					// Prove that the rewrite is internally coherent. The failure must
					// come from the pre-mutation snapshot, not a malformed receipt.
					if after, err := manager.Active(ctx); err != nil || after != active {
						t.Fatalf("coordinated rewrite must still pass Manager.Active: %+v, %v", after, err)
					}
					if err := verifyHermesSource(ctx, f.source, filepath.Join(versionDir, "source")); err != nil {
						t.Fatalf("mutation unexpectedly changed pinned source: %v", err)
					}
					return nil
				}
				err := f.run(ctx, "install", hermesAgentArgs()...)
				if !reached {
					t.Fatalf("bootstrap mutation boundary not reached: %v", err)
				}
				if len(f.commands) != 0 {
					t.Fatalf("coordinated UV/receipt mutation reached execution: commands=%v error=%v", f.commands, err)
				}
				if err == nil {
					t.Fatal("coordinated UV/receipt mutation was accepted")
				}
			})
		}
	}
}
