//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// All uncertainty after writing intent retains both locks and journal. Killing
// systemctl does not establish that the manager's submitted job was cancelled.
func (b *backend) ollamaServiceExecute(ctx context.Context, o options) (result error) {
	if o.provider != "ollama" || o.removePath || o.refreshLauncher || o.trust || o.rocm || o.acceptPM || o.acceptSystem || o.keepPreparation || (o.version != "" && o.version != "latest") {
		return fmt.Errorf("invalid private service route")
	}
	switch o.serviceAction {
	case "status":
		if o.acceptService {
			return fmt.Errorf("readonly service status accepts no consent")
		}
	case "register", "start", "stop", "remove":
		if !o.acceptService {
			return fmt.Errorf("service execution consent required")
		}
	default:
		return fmt.Errorf("unsupported service action")
	}
	if b.serviceTools().uid() <= 0 {
		return fmt.Errorf("Ollama user service refuses root")
	}
	home, err := ollamaServiceHome()
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = absolutePath(b.ollamaServiceRoot()); err != nil {
		return err
	}
	if o.serviceAction == "status" {
		s, err := b.inspectOllamaService(home)
		if err != nil {
			return err
		}
		if err = b.servicePaths(ctx, home, s.Receipt != nil); err != nil {
			return err
		}
		m, err := b.serviceManager(ctx, home)
		if err != nil {
			return err
		}
		if s.Receipt == nil {
			err = m.absent()
		} else {
			err = m.owned(*s.Receipt)
		}
		if err != nil {
			return err
		}
		b.log(fmt.Sprintf(label("ollama_service_report"), m["LoadState"], m["ActiveState"], m["SubState"]))
		return nil
	}
	if err = dirs(filepath.Dir(b.root("ollama")), false, false); err != nil {
		return err
	}
	locks := []string{b.root("ollama") + ".lock", filepath.Join(home, ".atm-ai-path.lock")}
	owned := map[string]os.FileInfo{}
	evidence := map[string]pathState{}
	retain := false
	homeInfo, err := os.Lstat(home)
	if err != nil {
		return err
	}
	checkLocks := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := ollamaServiceHome()
		if err != nil || h != home {
			return fmt.Errorf("service HOME changed")
		}
		i, err := os.Lstat(home)
		if err != nil || !os.SameFile(homeInfo, i) {
			return fmt.Errorf("service HOME replaced")
		}
		for p, old := range owned {
			if err := dirs(p, false, true); err != nil {
				return err
			}
			now, err := os.Lstat(p)
			if err != nil || !os.SameFile(old, now) {
				return fmt.Errorf("service lock replaced: %s", p)
			}
		}
		for p, old := range evidence {
			now, err := pathSnapshot(p, false)
			if err != nil || !pathUnchanged(now, old) {
				return fmt.Errorf("service journal changed: %s", p)
			}
		}
		return nil
	}
	defer func() {
		if e := checkLocks(); e != nil {
			retain = true
			result = errors.Join(result, e)
		}
		if !retain {
			for p := range evidence {
				if e := os.Remove(p); e != nil {
					retain = true
					result = errors.Join(result, e)
					break
				}
			}
		}
		if !retain {
			for i := len(locks) - 1; i >= 0; i-- {
				if _, ok := owned[locks[i]]; ok {
					if e := os.Remove(locks[i]); e != nil {
						retain = true
						result = errors.Join(result, e)
						break
					}
				}
			}
		}
		if retain {
			result = errors.Join(result, fmt.Errorf("service state uncertain; journals/locks retained at %s and %s; inspect manually, no rollback", locks[0], locks[1]))
		}
	}()
	for _, p := range locks {
		if err = os.Mkdir(p, 0700); err != nil {
			return fmt.Errorf("service/provider/HOME locked: %s: %w", p, err)
		}
		i, e := os.Lstat(p)
		if e != nil {
			retain = true
			return e
		}
		owned[p] = i
	}
	s, err := b.inspectOllamaService(home)
	if err != nil {
		return err
	}
	if o.serviceAction == "register" && s.Receipt != nil {
		return fmt.Errorf("service already registered; stop and remove it before registering")
	}
	if o.serviceAction != "register" && s.Receipt == nil {
		return fmt.Errorf("no owned registered service")
	}
	// Query the actual login manager before creating any unit or data. Changing
	// the client's HOME does not change the user manager's unit search path.
	if err = b.servicePaths(ctx, home, s.Receipt != nil); err != nil {
		return err
	}
	m, err := b.serviceManager(ctx, home)
	if err != nil {
		return err
	}
	var r ollamaServiceReceipt
	if s.Receipt == nil {
		if err = m.absent(); err != nil {
			return err
		}
		for _, p := range []string{ollamaServiceAnchor(home), ollamaServiceUnitPath(home)} {
			if _, e := os.Lstat(p); !os.IsNotExist(e) {
				return fmt.Errorf("preexisting service evidence preserved: %s", p)
			}
		}
		_, v, e := b.ollamaInspect(ctx)
		if e != nil {
			return e
		}
		if v == "" {
			return fmt.Errorf("select an owned verified portable Ollama bundle first")
		}
		bundle, e := b.ollamaVerify(ctx, v, b.ollamaVersion(v))
		if e != nil {
			return e
		}
		env, e := b.serviceTools().resolve("env")
		if e != nil {
			return e
		}
		// The final owner includes newly created inode identities, filled below.
		r = b.serviceReceipt(home, v, ollamaServiceHash(bundle), env, ollamaServiceOwner{})
	} else {
		r = *s.Receipt
		if err = m.owned(r); err != nil {
			return err
		}
		if o.serviceAction == "remove" && (m["ActiveState"] != "inactive" || m["SubState"] != "dead") {
			return fmt.Errorf("explicit stop required before removing service registration")
		}
		if o.serviceAction == "start" {
			if m["ActiveState"] != "inactive" || m["SubState"] != "dead" {
				return fmt.Errorf("service start requires inactive/dead state; stop explicitly first")
			}
			if err = b.servicePayload(ctx, r); err != nil {
				return err
			}
			env, e := b.serviceTools().resolve("env")
			if e != nil || env != r.Env {
				return fmt.Errorf("trusted env executable identity changed: %v", e)
			}
		}
	}
	if err = b.boundary("ollama-service-preflight"); err != nil {
		return err
	}
	if err = checkLocks(); err != nil {
		return err
	}
	if err = ollamaServiceUnchanged(s); err != nil {
		return err
	}
	if err = b.servicePaths(ctx, home, s.Receipt != nil); err != nil {
		return err
	}
	checked, err := b.serviceManager(ctx, home)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(m, checked) {
		return fmt.Errorf("manager state changed during service preflight")
	}
	if o.serviceAction == "register" || o.serviceAction == "start" {
		if err = b.servicePayload(ctx, r); err != nil {
			return err
		}
	}
	journal := struct {
		Schema  int
		Action  string
		Receipt ollamaServiceReceipt
	}{1, o.serviceAction, r}
	record := func(p string) error {
		retain = true
		if e := writeJSON(p, journal); e != nil {
			return e
		}
		state, e := pathSnapshot(p, false)
		if e != nil {
			return e
		}
		evidence[p] = state
		return nil
	}
	if err = record(filepath.Join(locks[0], "ollama-service.json")); err != nil {
		return err
	}
	root := b.ollamaServiceRoot()
	if o.serviceAction == "register" {
		if s.Owner == nil {
			if err = os.Mkdir(root, 0700); err != nil {
				return err
			}
			for _, rel := range []string{"data", "data/home", "data/models"} {
				if err = os.Mkdir(filepath.Join(root, rel), 0700); err != nil {
					return err
				}
			}
			own, e := b.serviceOwner(home)
			if e != nil {
				return e
			}
			if err = writeJSON(filepath.Join(root, "owner.json"), own); err != nil {
				return err
			}
			s.Owner = &own
		}
		r = b.serviceReceipt(home, r.Version, r.BundleSHA256, r.Env, *s.Owner)
		journal.Receipt = r
	}
	if err = record(filepath.Join(root, "journal.json")); err != nil {
		return err
	}
	if err = b.boundary("ollama-service-before-mutation"); err != nil {
		return err
	}
	if err = checkLocks(); err != nil {
		return err
	}
	if err = ollamaServiceUnchanged(s); err != nil {
		return err
	}
	own, err := b.serviceOwner(home)
	if err != nil || !reflect.DeepEqual(own, *s.Owner) {
		return fmt.Errorf("service data identity changed before mutation: %v", err)
	}
	if err = b.servicePaths(ctx, home, s.Receipt != nil); err != nil {
		return err
	}
	checked, err = b.serviceManager(ctx, home)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(m, checked) {
		return fmt.Errorf("manager state changed before mutation")
	}
	if o.serviceAction == "register" || o.serviceAction == "start" {
		if err = b.servicePayload(ctx, r); err != nil {
			return err
		}
	}
	switch o.serviceAction {
	case "register":
		if err = dirs(filepath.Dir(r.UnitPath), true, false); err != nil {
			return err
		}
		if err = writeJSON(filepath.Join(root, "receipt.json"), r); err != nil {
			return err
		}
		if err = writeJSON(ollamaServiceAnchor(home), r); err != nil {
			return err
		}
		if err = ollamaServiceWriteUnit(r.UnitPath, ollamaServiceUnit(r)); err != nil {
			return err
		}
		if _, err = b.serviceCommand(ctx, home, "daemon-reload"); err != nil {
			return err
		}
		checked, err = b.serviceManager(ctx, home)
		if err != nil {
			return err
		}
		if err = checked.owned(r); err != nil {
			return err
		}
		if checked["ActiveState"] != "inactive" || checked["SubState"] != "dead" {
			return fmt.Errorf("service unexpectedly active after registration")
		}
	case "start", "stop":
		if o.serviceAction == "start" {
			// Reload even when timestamps claim freshness: this binds the manager's
			// complete execution context to the exact verified unit, including ':'
			// flags not exposed by the legacy ExecStart property representation.
			if _, err = b.serviceCommand(ctx, home, "daemon-reload"); err != nil {
				return err
			}
			if err = b.boundary("ollama-service-before-start"); err != nil {
				return err
			}
			if err = checkLocks(); err != nil {
				return err
			}
			if err = ollamaServiceUnchanged(s); err != nil {
				return err
			}
			currentOwner, e := b.serviceOwner(home)
			if e != nil || !reflect.DeepEqual(currentOwner, *s.Owner) {
				return fmt.Errorf("service data identity changed during start reload: %v", e)
			}
			if err = b.servicePaths(ctx, home, true); err != nil {
				return err
			}
			if err = b.servicePayload(ctx, r); err != nil {
				return err
			}
			checked, err = b.serviceManager(ctx, home)
			if err != nil {
				return err
			}
			if err = checked.owned(r); err != nil {
				return err
			}
			if checked["ActiveState"] != "inactive" || checked["SubState"] != "dead" {
				return fmt.Errorf("service changed state during start reload")
			}
		}
		if _, err = b.serviceCommand(ctx, home, o.serviceAction); err != nil {
			return err
		}
		checked, err = b.serviceManager(ctx, home)
		if err != nil {
			return err
		}
		if err = checked.owned(r); err != nil {
			return err
		}
		want := "inactive/dead"
		if o.serviceAction == "start" {
			want = "active/running"
		}
		if checked["ActiveState"]+"/"+checked["SubState"] != want {
			return fmt.Errorf("manager did not reach expected stable state")
		}
	case "remove":
		if err = ollamaServiceRemoveExact(r.UnitPath, s.Files[r.UnitPath]); err != nil {
			return err
		}
		if _, err = b.serviceCommand(ctx, home, "daemon-reload"); err != nil {
			return err
		}
		checked, err = b.serviceManager(ctx, home)
		if err != nil {
			return err
		}
		if err = checked.absent(); err != nil {
			return err
		}
		if err = b.servicePaths(ctx, home, false); err != nil {
			return err
		}
		if err = checkLocks(); err != nil {
			return err
		}
		if err = ollamaServiceRemoveExact(filepath.Join(root, "receipt.json"), s.Files[filepath.Join(root, "receipt.json")]); err != nil {
			return err
		}
		if err = ollamaServiceRemoveExact(ollamaServiceAnchor(home), s.Files[ollamaServiceAnchor(home)]); err != nil {
			return err
		}
	}
	if err = b.boundary("ollama-service-after-mutation"); err != nil {
		return err
	}
	if err = checkLocks(); err != nil {
		return err
	}
	own, err = b.serviceOwner(home)
	if err != nil || !reflect.DeepEqual(own, *s.Owner) {
		return fmt.Errorf("service data identity changed after mutation: %v", err)
	}
	if o.serviceAction != "remove" {
		if err = b.servicePaths(ctx, home, true); err != nil {
			return err
		}
		if o.serviceAction != "register" {
			if err = ollamaServiceUnchanged(s); err != nil {
				return err
			}
		}
		var recordedOwner ollamaServiceOwner
		if err = readJSON(filepath.Join(root, "owner.json"), &recordedOwner); err != nil || !reflect.DeepEqual(recordedOwner, own) {
			return fmt.Errorf("service owner changed after manager command: %v", err)
		}
		for _, p := range []string{filepath.Join(root, "receipt.json"), ollamaServiceAnchor(home)} {
			var value ollamaServiceReceipt
			if err = readJSON(p, &value); err != nil {
				return err
			}
			if !reflect.DeepEqual(value, r) {
				return fmt.Errorf("service evidence changed after manager command")
			}
		}
		unit, e := pathSnapshot(r.UnitPath, false)
		if e != nil || unit.Mode != 0600 || string(unit.Data) != string(ollamaServiceUnit(r)) {
			return fmt.Errorf("service unit changed after manager command")
		}
	}
	retain = false
	b.log(label("ollama_service_" + o.serviceAction + "_done"))
	return nil
}

func ollamaServiceWriteUnit(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func ollamaServiceRemoveExact(path string, old pathState) error {
	now, err := pathSnapshot(path, false)
	if err != nil || !old.Exists || !pathUnchanged(now, old) {
		return fmt.Errorf("owned service entry changed before removal: %s", path)
	}
	return os.Remove(path)
}

// No manager calls here. Any registered or uncertain metadata blocks every
// portable version mutation, even inactive versions and internal entry points.
func (b *backend) ollamaServiceMutationGuard() error {
	home, err := integrationHome()
	if err != nil {
		return err
	}
	for _, p := range []string{ollamaServiceAnchor(home), ollamaServiceUnitPath(home), filepath.Join(b.ollamaServiceRoot(), "receipt.json"), filepath.Join(b.ollamaServiceRoot(), "journal.json"), filepath.Join(b.root("ollama")+".lock", "ollama-service.json")} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return fmt.Errorf("Ollama service registered or uncertain; stop and remove the service first: %s", p)
		}
	}
	lock := filepath.Join(home, ".atm-ai-path.lock")
	i, err := os.Lstat(lock)
	if err == nil {
		if b.ollamaLifecycleLock == nil || !os.SameFile(b.ollamaLifecycleLock, i) {
			return fmt.Errorf("AI profiles locked/interrupted; Ollama HOME shared lock")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	s, err := b.inspectOllamaService(home)
	if err != nil {
		return err
	}
	if s.Receipt != nil {
		return fmt.Errorf("Ollama service remains registered")
	}
	return nil
}

func (b *backend) ollamaLifecycle(ctx context.Context, fn func() error) (result error) {
	if err := b.ollamaServiceMutationGuard(); err != nil {
		return err
	}
	if b.ollamaLifecycleLock != nil {
		return fn()
	}
	home, err := integrationHome()
	if err != nil {
		return err
	}
	lock := filepath.Join(home, ".atm-ai-path.lock")
	if err = os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("Ollama HOME locked: %w", err)
	}
	i, err := os.Lstat(lock)
	if err != nil {
		return err
	}
	b.ollamaLifecycleLock = i
	defer func() {
		b.ollamaLifecycleLock = nil
		now, e := os.Lstat(lock)
		if e != nil || !os.SameFile(i, now) {
			result = errors.Join(result, fmt.Errorf("Ollama HOME lock changed; retained"))
			return
		}
		result = errors.Join(result, os.Remove(lock))
	}()
	if err = b.ollamaServiceMutationGuard(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return fn()
}
