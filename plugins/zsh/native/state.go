//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type moveRecord struct {
	Target   string `json:"target"`
	Staged   string `json:"staged"`
	Recovery string `json:"recovery"`
}
type operation struct {
	Schema   int          `json:"schema"`
	Action   string       `json:"action"`
	Phase    string       `json:"phase"`
	Snapshot string       `json:"snapshot,omitempty"`
	Moves    []moveRecord `json:"moves,omitempty"`
	Staging  []string     `json:"staging,omitempty"`
	Updated  string       `json:"updated"`
	dir      string
}

func (o *operation) stage(parent string) (string, error) {
	p, e := os.MkdirTemp(parent, ".atm-zsh-stage-*")
	if e != nil {
		return "", e
	}
	o.Staging = append(o.Staging, p)
	if e = o.save(); e != nil {
		return "", e
	}
	return p, nil
}
func (b *backend) begin(action string) (*operation, error) {
	if e := dirs(b.state, true, true); e != nil {
		return nil, e
	}
	p := filepath.Join(b.state, "operation")
	if e := os.Mkdir(p, 0700); e != nil {
		return nil, fmt.Errorf("operation/interrupted lock exists; inspect journal, package processes and recovery before manual recovery: %w", e)
	}
	o := &operation{Schema: 1, Action: action, Phase: "prepared", dir: p}
	if e := o.save(); e != nil {
		return nil, e
	}
	return o, nil
}
func (o *operation) save() error {
	o.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	v, e := json.Marshal(o)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(o.dir, ".journal-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
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
	return os.Rename(name, filepath.Join(o.dir, "journal.json"))
}
func (o *operation) phase(s string) error { o.Phase = s; return o.save() }
func (o *operation) finish() error {
	if e := o.phase("completed"); e != nil {
		return e
	}
	if e := os.Remove(filepath.Join(o.dir, "journal.json")); e != nil {
		return e
	}
	return os.Remove(o.dir)
}
func (b *backend) invalidateReceipt(o *operation) error {
	p := filepath.Join(b.state, "receipt.json")
	if _, e := os.Lstat(p); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	// A snapshot is user data, never authority to own or remove an installation.
	if e := o.phase("invalidating-installation-receipt"); e != nil {
		return e
	}
	return b.rename(p, filepath.Join(o.dir, "invalidated-receipt.json"))
}
