package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func absolutePath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
		return fmt.Errorf("clean absolute file path required")
	}
	for _, r := range p {
		if r < 32 || r == 127 {
			return fmt.Errorf("control character in file path")
		}
	}
	return nil
}
func realDirs(path string, create bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("absolute directory required")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		i, e := os.Lstat(current)
		if os.IsNotExist(e) && create {
			if e = os.Mkdir(current, 0700); e != nil {
				return e
			}
			i, e = os.Lstat(current)
		}
		if e != nil {
			return e
		}
		if !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked/non-directory ancestor refused: %s", current)
		}
	}
	return nil
}
func readRegular(path string, limit int64) ([]byte, error) {
	if err := absolutePath(path); err != nil {
		return nil, err
	}
	if err := realDirs(filepath.Dir(path), false); err != nil {
		return nil, err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !i.Mode().IsRegular() || i.Size() > limit {
		return nil, fmt.Errorf("regular bounded data file required: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if len(b) > int(limit) {
		return nil, fmt.Errorf("file exceeds limit")
	}
	return b, err
}

type operationRecord struct {
	Schema   int      `json:"schema"`
	Plugin   string   `json:"plugin"`
	Action   string   `json:"action"`
	System   string   `json:"system"`
	Manager  string   `json:"manager"`
	Packages []string `json:"packages"`
	Plan     string   `json:"plan"`
	Status   string   `json:"status"`
	Phase    string   `json:"phase"`
	Updated  string   `json:"updated"`
}

func readRecord(path string) (*operationRecord, []byte, error) {
	if err := journalDirectory(filepath.Dir(path), false); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	contents, err := readRegular(path, 128<<10)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	i, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if i.Mode().Perm() != 0600 || !ownedByCurrentUser(i) {
		return nil, nil, fmt.Errorf("operation journal must be owned by the current user with private mode 0600")
	}
	dec := json.NewDecoder(bytes.NewReader(contents))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, nil, fmt.Errorf("invalid journal object")
	}
	seen := map[string]bool{}
	for dec.More() {
		key, e := dec.Token()
		if e != nil {
			return nil, nil, e
		}
		s, ok := key.(string)
		if !ok || seen[s] {
			return nil, nil, fmt.Errorf("duplicate journal key")
		}
		seen[s] = true
		var value json.RawMessage
		if e = dec.Decode(&value); e != nil {
			return nil, nil, e
		}
	}
	if _, err = dec.Token(); err != nil {
		return nil, nil, err
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, nil, fmt.Errorf("trailing journal data")
	}
	r := &operationRecord{}
	dec = json.NewDecoder(bytes.NewReader(contents))
	dec.DisallowUnknownFields()
	if err = dec.Decode(r); err != nil {
		return nil, nil, err
	}
	if r.Schema != 1 || r.Plugin != "os_packages" || (r.Action != "install" && r.Action != "remove") || (r.Status != "running" && r.Status != "failed" && r.Status != "completed") {
		return nil, nil, fmt.Errorf("foreign/invalid journal; preserve and inspect it")
	}
	return r, contents, nil
}

type journal struct {
	path     string
	expected []byte
}

func openJournal(path string) (*journal, func(), error) {
	if err := absolutePath(path); err != nil {
		return nil, nil, err
	}
	if err := journalDirectory(filepath.Dir(path), true); err != nil {
		return nil, nil, err
	}
	lock := filepath.Join(filepath.Dir(path), ".native-lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		return nil, nil, fmt.Errorf("another operation or interrupted lock exists at %s; inspect system package state before manual recovery: %w", lock, err)
	}
	release := func() { _ = os.Remove(lock) }
	record, old, err := readRecord(path)
	if err != nil {
		release()
		return nil, nil, err
	}
	if record != nil && record.Status == "running" {
		release()
		return nil, nil, fmt.Errorf("previous operation has no final outcome; inspect active processes/package state and preserve/archive the journal before retrying")
	}
	return &journal{path, old}, release, nil
}
func (j *journal) save(r operationRecord) error {
	_, now, err := readRecord(j.path)
	if err != nil {
		return err
	}
	if !bytes.Equal(now, j.expected) {
		return fmt.Errorf("journal changed concurrently; preserving it")
	}
	r.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	content, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(j.path), ".operation-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(content); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	_, now, err = readRecord(j.path)
	if err != nil {
		return err
	}
	if !bytes.Equal(now, j.expected) {
		return fmt.Errorf("journal changed before publication")
	}
	if j.expected == nil {
		err = os.Link(f.Name(), j.path)
	} else {
		err = os.Rename(f.Name(), j.path)
	}
	if err == nil {
		j.expected = content
	}
	return err
}
