package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/AtomyCloud/atm/internal/protocol"
	"os"
	"path/filepath"
	"time"
)

type operationRecord struct {
	Schema   int    `json:"schema"`
	Plugin   string `json:"plugin"`
	Action   string `json:"action"`
	Status   string `json:"status"`
	Phase    string `json:"phase"`
	Plan     string `json:"plan"`
	Staging  string `json:"staging,omitempty"`
	Inputs   string `json:"inputs,omitempty"`
	Recovery string `json:"recovery,omitempty"`
	Target   string `json:"target,omitempty"`
	Updated  string `json:"updated"`
}

func readRecord(path string) (*operationRecord, []byte, error) {
	if e := realDirs(filepath.Dir(path), false, true); e != nil {
		if os.IsNotExist(e) {
			return nil, nil, nil
		}
		return nil, nil, e
	}
	v, e := readRegular(path, 32<<10)
	if os.IsNotExist(e) {
		return nil, nil, nil
	}
	if e != nil {
		return nil, nil, e
	}
	i, e := os.Lstat(path)
	if e != nil {
		return nil, nil, e
	}
	if !ownedByCurrentUser(i) || i.Mode().Perm() != 0600 {
		return nil, nil, fmt.Errorf("private owned journal required")
	}
	var r operationRecord
	if e = protocol.Decode(v, &r); e != nil {
		return nil, nil, e
	}
	if r.Schema != 1 || r.Plugin != "brave" || (r.Status != "running" && r.Status != "completed" && r.Status != "failed") {
		return nil, nil, fmt.Errorf("foreign/invalid journal")
	}
	switch r.Action {
	case "install", "remove", "backup-profile", "backup-bookmarks", "restore":
	default:
		return nil, nil, fmt.Errorf("foreign journal action")
	}
	return &r, v, nil
}

type journal struct {
	path     string
	expected []byte
}

func openJournal(path string) (*journal, func(), error) {
	if e := realDirs(filepath.Dir(path), true, true); e != nil {
		return nil, nil, e
	}
	lock := filepath.Join(filepath.Dir(path), ".native-lock")
	if e := os.Mkdir(lock, 0700); e != nil {
		return nil, nil, fmt.Errorf("operation/interrupted lock exists; inspect journal and processes before manual recovery: %w", e)
	}
	release := func() { _ = os.Remove(lock) }
	r, old, e := readRecord(path)
	if e != nil {
		release()
		return nil, nil, e
	}
	if r != nil && r.Status == "running" {
		release()
		return nil, nil, fmt.Errorf("unfinished operation; preserve journal/staging/recovery and inspect before manual recovery")
	}
	return &journal{path, old}, release, nil
}
func (j *journal) save(r operationRecord) error {
	_, now, e := readRecord(j.path)
	if e != nil {
		return e
	}
	if !bytes.Equal(now, j.expected) {
		return fmt.Errorf("journal changed; preserving it")
	}
	r.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	v, e := json.Marshal(r)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(j.path), ".journal-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(v); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	_, now, e = readRecord(j.path)
	if e != nil {
		return e
	}
	if !bytes.Equal(now, j.expected) {
		return fmt.Errorf("journal changed before publication; preserving it")
	}
	if j.expected == nil {
		e = os.Link(f.Name(), j.path)
	} else {
		e = os.Rename(f.Name(), j.path)
	}
	if e != nil {
		return e
	}
	j.expected = v
	return syncDir(filepath.Dir(j.path))
}
