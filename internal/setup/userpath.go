package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pathValue struct {
	Value  string `json:"value"`
	Type   uint32 `json:"type"`
	Exists bool   `json:"exists"`
}

type pathStore interface {
	Read() (pathValue, error)
	Write(pathValue) error
	Close() error
}

func prependUserPath(old pathValue, entry string) (pathValue, error) {
	if strings.ContainsAny(entry, ";\x00\r\n\"") || len(entry) == 0 {
		return old, fmt.Errorf("unsafe Windows PATH entry")
	}
	if old.Exists && old.Type != 1 && old.Type != 2 {
		return old, fmt.Errorf("user PATH has unsupported registry type")
	}
	if old.Type == 2 && strings.Contains(entry, "%") {
		return old, fmt.Errorf("literal percent is unsafe in expandable PATH")
	}
	for _, p := range strings.Split(old.Value, ";") {
		if strings.EqualFold(strings.TrimRight(p, "\\/"), strings.TrimRight(entry, "\\/")) {
			return old, nil
		}
	}
	next := pathValue{entry, old.Type, true}
	if !old.Exists {
		next.Type = 1
	}
	if old.Value != "" {
		next.Value += ";" + old.Value
	}
	if len(next.Value) > 16000 || strings.ContainsAny(old.Value, "\x00\r\n") {
		return old, fmt.Errorf("user PATH exceeds limit or contains control characters")
	}
	return next, nil
}

// The store boundary permits tests without touching any real registry hive.
func integrateUserPath(ctx context.Context, home *os.Root, o Options, result Result, store pathStore) (Result, error) {
	defer store.Close()
	old, err := store.Read()
	if err != nil {
		return result, err
	}
	next, err := prependUserPath(old, result.Destination)
	if err != nil {
		return result, err
	}
	if next == old {
		return result, nil
	}
	const target = `HKCU\Environment\Path`
	result.Planned = []string{target}
	if o.DryRun {
		return result, nil
	}
	if !o.Consent {
		return result, ErrConsent
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	unlock, err := lock(home)
	if err != nil {
		return result, err
	}
	defer unlock()
	backup, err := privateDir(home, ".atm-setup-backup-")
	if err != nil {
		return result, err
	}
	b, err := json.Marshal(old)
	if err != nil {
		return result, err
	}
	name := filepath.Join(backup, "user-path.json")
	if err = writeNew(home, name, b, 0600); err != nil {
		return result, err
	}
	result.Backups = []string{filepath.Join(o.UserHome, name)}
	current, err := store.Read()
	if err != nil {
		return result, err
	}
	if current != old {
		return result, fmt.Errorf("user PATH changed concurrently; backup retained")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = store.Write(next); err != nil {
		return result, err
	}
	result.Changed = []string{target}
	actual, err := store.Read()
	if err != nil {
		return result, err
	}
	if actual != next {
		return result, fmt.Errorf("user PATH readback failed; backup retained")
	}
	return result, nil
}
