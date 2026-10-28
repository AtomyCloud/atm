package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const pathReceipt = ".atm-rea-path.json"
const pathStart = "# >>> ATM native REA PATH >>>"
const pathEnd = "# <<< ATM native REA PATH <<<"

func contentEntry(data []byte, mode os.FileMode) entry {
	h := sha256.Sum256(data)
	return entry{Mode: uint32(mode), Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])}
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (b *backend) pathBlock() string {
	return pathStart + "\nexport PATH=" + shellQuote(filepath.Join(b.root, "bin")) + ":\"$PATH\"\n" + pathEnd + "\n"
}
func (b *backend) integrationChanges(remove bool) ([]change, error) {
	if err := realDirs(b.home, false); err != nil {
		return nil, err
	}
	record := filepath.Join(b.root, pathReceipt)
	snap, err := snapshot(record)
	if err != nil {
		return nil, err
	}
	owned := false
	if snap.Exists {
		var block string
		if snap.Mode != 0600 || decodeJSON(snap.Data, &block, true) != nil || block != b.pathBlock() {
			return nil, fmt.Errorf("changed PATH ownership record")
		}
		owned = true
	}
	if remove && !owned {
		return nil, nil
	}
	var changes []change
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		file := filepath.Join(b.home, name)
		before, e := snapshot(file)
		if e != nil {
			return nil, e
		}
		if before.Link != "" {
			return nil, fmt.Errorf("symlink profile preserved")
		}
		if len(before.Data) > 1<<20 {
			return nil, fmt.Errorf("oversized profile")
		}
		if !before.Exists && (remove || name == ".zshrc") {
			continue
		}
		s := string(before.Data)
		start, end := strings.Index(s, pathStart), strings.Index(s, pathEnd)
		if strings.Count(s, pathStart) > 1 || strings.Count(s, pathEnd) > 1 || (start < 0) != (end < 0) || end < start {
			return nil, fmt.Errorf("malformed PATH block preserved")
		}
		if start >= 0 {
			end += len(pathEnd)
			if !owned || (start > 0 && s[start-1] != '\n') || (end < len(s) && s[end] != '\n') || s[start:end] != strings.TrimSuffix(b.pathBlock(), "\n") {
				return nil, fmt.Errorf("edited/unowned PATH block preserved")
			}
			if end < len(s) {
				end++
			}
			s = s[:start] + s[end:]
		}
		if !remove {
			if len(s) > 0 && !strings.HasSuffix(s, "\n") {
				s += "\n"
			}
			s += b.pathBlock()
		}
		if bytes.Equal(before.Data, []byte(s)) {
			continue
		}
		after := regularState([]byte(s))
		if before.Exists {
			after.Mode = before.Mode
		}
		changes = append(changes, change{file, before, after})
	}
	after := fileState{}
	if !remove {
		data, _ := json.Marshal(b.pathBlock())
		after = regularState(data)
	}
	changes = append(changes, change{record, snap, after})
	return changes, nil
}
func (b *backend) integrate(ctx context.Context) error {
	if err := b.preflight(ctx); err != nil {
		return err
	}
	v, err := b.current()
	if err != nil {
		return err
	}
	if v == "" {
		return fmt.Errorf("no native REA selected")
	}
	if err = b.verifyOwned(ctx, v); err != nil {
		return err
	}
	changes, err := b.integrationChanges(false)
	if err != nil {
		return err
	}
	_, err = applyChanges(ctx, changes)
	if err == nil {
		b.log("REA PATH block installed in user profiles; reload the shell. Existing external commands and agent integrations are unchanged.")
	}
	return err
}
