package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const localReceipt = ".atm-local-entry.json"
const pathReceipt = ".atm-path.json"

func (b *backend) localTarget() string {
	return filepath.Join(b.localRoot, "native-v1/current/bin/spotatui")
}
func (b *backend) localOwned() (bool, error) {
	data, mode, exists, err := readProfile(filepath.Join(b.root, localReceipt))
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	want, _ := json.Marshal(b.localEntry)
	if mode != 0600 || !bytes.Equal(data, want) {
		return false, fmt.Errorf("invalid local entry ownership record")
	}
	return true, nil
}
func (b *backend) checkLocalEntry() error {
	if err := realDirs(filepath.Dir(b.localEntry), false); err != nil && !os.IsNotExist(err) {
		return err
	}
	owned, err := b.localOwned()
	if err != nil {
		return err
	}
	_, err = os.Lstat(b.localEntry)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	target, err := os.Readlink(b.localEntry)
	if err != nil || target != b.localTarget() || !owned {
		return fmt.Errorf("foreign/changed local entry preserved; move it yourself before native local installation: %s", b.localEntry)
	}
	return nil
}
func (b *backend) publishLocal(ctx context.Context) (func() error, error) {
	if err := b.checkLocalEntry(); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(b.localEntry); err == nil {
		return func() error { return nil }, nil
	}
	if err := realDirs(filepath.Dir(b.localEntry), true); err != nil {
		return nil, err
	}
	file := filepath.Join(b.root, localReceipt)
	before, mode, exists, err := readProfile(file)
	if err != nil {
		return nil, err
	}
	after, _ := json.Marshal(b.localEntry)
	undo, err := applyProfiles(ctx, []profilePlan{{file, before, after, mode, exists}})
	if err != nil {
		return nil, err
	}
	// Symlink creation is exclusive: a late foreign entry is never replaced.
	if err = ctx.Err(); err == nil {
		err = os.Symlink(b.localTarget(), b.localEntry)
	}
	if err != nil {
		if u := undo(); u != nil {
			return nil, fmt.Errorf("%v; ownership rollback: %w", err, u)
		}
		return nil, err
	}
	return func() error {
		if err := b.checkLocalEntry(); err != nil {
			return err
		}
		if err := os.Remove(b.localEntry); err != nil {
			return err
		}
		return undo()
	}, nil
}
func (b *backend) removeLocal() error {
	owned, err := b.localOwned()
	if err != nil || !owned {
		return err
	}
	if err = b.checkLocalEntry(); err != nil {
		return err
	}
	if _, err = os.Lstat(b.localEntry); err == nil {
		if err = os.Remove(b.localEntry); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Remove(filepath.Join(b.root, localReceipt))
}
func (b *backend) pathBlock() string {
	return b.profileStart() + "\n" + b.shellEnvironment("\n") + "\n" + b.profileEnd() + "\n"
}
func (b *backend) profileStart() string {
	return "# >>> ATM native spotatui " + b.location + " PATH >>>"
}
func (b *backend) profileEnd() string { return "# <<< ATM native spotatui " + b.location + " PATH <<<" }
func (b *backend) pathOwned() (bool, error) {
	data, mode, exists, err := readProfile(filepath.Join(b.root, pathReceipt))
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	want, _ := json.Marshal(b.pathBlock())
	if mode != 0600 || !bytes.Equal(data, want) {
		return false, fmt.Errorf("changed PATH ownership record")
	}
	return true, nil
}
func (b *backend) integrationPlans(remove bool) ([]profilePlan, error) {
	owned, err := b.pathOwned()
	if err != nil {
		return nil, err
	}
	if remove && !owned {
		return nil, nil
	}
	for _, name := range []string{".profile", ".bashrc", ".zshrc"} {
		data, _, _, err := readProfile(filepath.Join(b.home, name))
		if err != nil {
			return nil, err
		}
		start, end := strings.Index(string(data), b.profileStart()), strings.Index(string(data), b.profileEnd())
		if start >= 0 || end >= 0 {
			if !owned || start < 0 || end < start {
				return nil, fmt.Errorf("unowned/changed PATH block preserved")
			}
			end += len(b.profileEnd())
			actual := string(data[start:end])
			if actual != strings.TrimSuffix(b.pathBlock(), "\n") {
				return nil, fmt.Errorf("edited PATH block preserved")
			}
		}
	}
	return b.profilePlans(remove)
}
func (b *backend) integrate(ctx context.Context) error {
	v, err := b.current()
	if err != nil {
		return err
	}
	if v == "" {
		return fmt.Errorf("no native version selected")
	}
	if err = b.verifyOwned(ctx, v); err != nil {
		return err
	}
	plans, err := b.integrationPlans(false)
	if err != nil {
		return err
	}
	file := filepath.Join(b.root, pathReceipt)
	before, mode, exists, err := readProfile(file)
	if err != nil {
		return err
	}
	after, _ := json.Marshal(b.pathBlock())
	plans = append(plans, profilePlan{file, before, after, mode, exists})
	_, err = applyProfiles(ctx, plans)
	if err == nil {
		b.log("User PATH configured; refresh this shell: " + b.shellRefresh())
	}
	return err
}
func (b *backend) removeIntegration(ctx context.Context) error {
	owned, err := b.pathOwned()
	if err != nil || !owned {
		return err
	}
	plans, err := b.integrationPlans(true)
	if err != nil {
		return err
	}
	undo, err := applyProfiles(ctx, plans)
	if err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(b.root, pathReceipt)); err != nil {
		if u := undo(); u != nil {
			return fmt.Errorf("%v; PATH rollback: %w", err, u)
		}
		return err
	}
	return nil
}
