package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AtomyCloud/atm/internal/plugin"
)

// Manager owns only a dedicated, newly created root. It never adopts legacy installs.
// Confirmation belongs to the caller. DryRun validates a plan without I/O.
type Manager struct {
	Root     string
	PluginID string
	DryRun   bool
	Limits   Limits
	Client   *http.Client
	// Tests inject failures at transaction boundaries; not an application callback.
	checkpoint func(string) error
}

type Result struct {
	Version    string `json:"version"`
	Path       string `json:"path"`
	Generation int    `json:"generation"`
	DryRun     bool   `json:"dry_run"`
}

type owner struct {
	Schema   int    `json:"schema"`
	PluginID string `json:"plugin_id"`
}
type receipt struct {
	Schema   int             `json:"schema"`
	PluginID string          `json:"plugin_id"`
	Artifact Spec            `json:"artifact"`
	Files    map[string]File `json:"files"`
}
type activation struct {
	Schema        int    `json:"schema"`
	Version       string `json:"version"`
	ReceiptSHA256 string `json:"receipt_sha256"`
	Generation    int    `json:"generation"`
}
type store struct {
	root *os.Root
	lock *os.File
}

func (s *store) close() { s.lock.Close(); s.root.Close() }

func (m Manager) validate() error {
	root := filepath.Clean(m.Root)
	if !filepath.IsAbs(root) || filepath.Dir(root) == root || !plugin.IDPattern.MatchString(m.PluginID) {
		return fmt.Errorf("a dedicated absolute root and valid plugin ID are required")
	}
	l := m.Limits.defaults()
	if l.DownloadBytes > 1<<30 || l.ExpandedBytes > 8<<30 || l.FileBytes > 2<<30 || l.Entries > 100000 {
		return fmt.Errorf("installation resource limits exceed hard caps")
	}
	return nil
}

func (m Manager) boundary(name string) error {
	if m.checkpoint != nil {
		return m.checkpoint(name)
	}
	return nil
}

func (m Manager) open(create bool) (*store, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	m.Root = filepath.Clean(m.Root)
	info, err := os.Lstat(m.Root)
	created := false
	if os.IsNotExist(err) && create {
		if err = os.Mkdir(m.Root, 0700); err != nil {
			return nil, err
		}
		created = true
	} else if err != nil {
		return nil, err
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("managed root must be a real directory")
	}
	r, err := os.OpenRoot(m.Root)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*store, error) { r.Close(); return nil, err }
	if created {
		if err = writeJSON(r, "owner.json", owner{1, m.PluginID}); err != nil {
			return fail(err)
		}
	}
	var o owner
	if _, err = readJSON(r, "owner.json", &o); err != nil || o.Schema != 1 || o.PluginID != m.PluginID {
		return fail(fmt.Errorf("unowned or incompatible installation root; no adoption"))
	}
	for _, name := range []string{"versions", "staging", "activations"} {
		if create {
			if err = r.MkdirAll(name, 0700); err != nil {
				return fail(err)
			}
		}
		info, err := r.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fail(fmt.Errorf("unsafe store directory: %s", name))
		}
	}
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	if info, err := r.Lstat("lock"); err == nil && !info.Mode().IsRegular() {
		return fail(fmt.Errorf("unsafe store lock"))
	} else if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	lock, err := r.OpenFile("lock", flags, 0600)
	if err != nil {
		return fail(err)
	}
	if err = lockFile(lock); err != nil {
		lock.Close()
		return fail(fmt.Errorf("installation busy: %w", err))
	}
	return &store{r, lock}, nil
}

func writeJSON(r *os.Root, name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > 8<<20 {
		return fmt.Errorf("state exceeds 8 MiB")
	}
	f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func readJSON(r *os.Root, name string, value any) (string, error) {
	limit := int64(4096)
	if _, ok := value.(*receipt); ok {
		limit = 8 << 20
	}
	info, err := r.Lstat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return "", fmt.Errorf("unsafe state file")
	}
	f, err := r.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > limit {
		return "", fmt.Errorf("state exceeds limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(value); err != nil {
		return "", err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return "", fmt.Errorf("trailing state data")
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

func stage(r *os.Root) (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	name := "staging/txn-" + hex.EncodeToString(id)
	return name, r.Mkdir(name, 0700)
}

func current(r *os.Root) (activation, error) {
	entries, err := fs.ReadDir(r.FS(), "activations")
	if err != nil {
		return activation{}, err
	}
	if len(entries) > 10000 {
		return activation{}, fmt.Errorf("activation history limit reached")
	}
	var a activation
	for i, e := range entries {
		if e.Name() != fmt.Sprintf("%020d.json", i+1) {
			return a, fmt.Errorf("activation history is incomplete or contains unknown files")
		}
		a = activation{}
		if _, err = readJSON(r, "activations/"+e.Name(), &a); err != nil {
			return a, err
		}
		b, err := hex.DecodeString(a.ReceiptSHA256)
		if err != nil || len(b) != 32 || a.Schema != 1 || a.Generation != i+1 || !validVersion(a.Version) {
			return a, fmt.Errorf("invalid activation record")
		}
	}
	return a, nil
}

func (m Manager) verify(ctx context.Context, r *os.Root, version string) (receipt, string, error) {
	var rec receipt
	base := "versions/" + version
	digest, err := readJSON(r, base+"/receipt.json", &rec)
	if err != nil {
		return rec, "", err
	}
	if rec.Schema != 1 || rec.PluginID != m.PluginID || rec.Artifact.Version != version || rec.Artifact.validate() != nil || len(rec.Files) == 0 || len(rec.Files) > m.Limits.defaults().Entries {
		return rec, "", fmt.Errorf("invalid installation receipt")
	}
	info, err := r.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return rec, "", fmt.Errorf("unsafe version directory")
	}
	info, err = r.Lstat(base + "/payload")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return rec, "", fmt.Errorf("unsafe payload directory")
	}
	payload, err := r.OpenRoot(base + "/payload")
	if err != nil {
		return rec, "", err
	}
	defer payload.Close()
	count, visited := 0, 0
	var total int64
	err = fs.WalkDir(payload.FS(), ".", func(name string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > m.Limits.defaults().Entries*16+1 {
			return fmt.Errorf("payload entry limit exceeded")
		}
		if e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		want, ok := rec.Files[name]
		if !ok || !portablePath(name) || !info.Mode().IsRegular() || info.Size() != want.Size || want.Size < 0 || want.Size > m.Limits.defaults().FileBytes || uint32(info.Mode().Perm()) != want.Mode {
			return fmt.Errorf("modified or unowned payload file: %s", name)
		}
		if want.Size > m.Limits.defaults().ExpandedBytes-total {
			return fmt.Errorf("payload size limit exceeded")
		}
		total += want.Size
		f, err := payload.Open(name)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, want.Size+1))
		f.Close()
		if err != nil {
			return err
		}
		if n != want.Size || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
			return fmt.Errorf("payload checksum mismatch: %s", name)
		}
		count++
		return nil
	})
	if err == nil && count != len(rec.Files) {
		err = fmt.Errorf("payload files are missing")
	}
	return rec, digest, err
}

// Install publishes an immutable version, then appends the activation commit.
// Failure before the activation commit never changes the previous active version.
func (m Manager) Install(ctx context.Context, spec Spec) (Result, error) {
	result := Result{Version: spec.Version, Path: filepath.Join(m.Root, "versions", spec.Version, "payload"), DryRun: m.DryRun}
	if err := m.validate(); err != nil {
		return result, err
	}
	if err := spec.validate(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if m.DryRun {
		return result, nil
	}
	s, err := m.open(true)
	if err != nil {
		return result, err
	}
	defer s.close()
	if err = stagingEmpty(s.root); err != nil {
		return result, err
	}
	if err = m.checkActive(ctx, s.root); err != nil {
		return result, err
	}
	versionPath := "versions/" + spec.Version
	if _, err = s.root.Lstat(versionPath); err == nil {
		rec, _, err := m.verify(ctx, s.root, spec.Version)
		if err != nil {
			return result, err
		}
		if rec.Artifact.SHA256 != spec.SHA256 || rec.Artifact.URL != spec.URL || rec.Artifact.Format != spec.Format || rec.Artifact.StripPrefix != spec.StripPrefix {
			return result, fmt.Errorf("existing version belongs to a different artifact")
		}
		return m.activate(ctx, s.root, spec.Version)
	} else if !os.IsNotExist(err) {
		return result, err
	}
	txn, err := stage(s.root)
	if err != nil {
		return result, err
	}
	defer s.root.RemoveAll(txn)
	if err = m.boundary("staged"); err != nil {
		return result, err
	}
	if err = download(ctx, s.root, txn+"/archive", spec, m.Limits.defaults(), m.Client); err != nil {
		return result, err
	}
	if err = s.root.MkdirAll(txn+"/version/payload", 0700); err != nil {
		return result, err
	}
	files, err := extract(ctx, s.root, txn+"/archive", txn+"/version/payload", spec, m.Limits.defaults())
	if err != nil {
		return result, err
	}
	if err = writeJSON(s.root, txn+"/version/receipt.json", receipt{1, m.PluginID, spec, files}); err != nil {
		return result, err
	}
	if err = m.boundary("verified"); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = s.root.Rename(txn+"/version", versionPath); err != nil {
		return result, err
	}
	if err = m.boundary("published"); err != nil {
		return result, err
	}
	return m.activate(ctx, s.root, spec.Version)
}

func stagingEmpty(r *os.Root) error {
	entries, err := fs.ReadDir(r.FS(), "staging")
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("interrupted installation found; explicit recovery required")
	}
	return nil
}

func (m Manager) checkActive(ctx context.Context, r *os.Root) error {
	a, err := current(r)
	if err != nil || a.Generation == 0 {
		return err
	}
	_, digest, err := m.verify(ctx, r, a.Version)
	if err != nil {
		return err
	}
	if digest != a.ReceiptSHA256 {
		return fmt.Errorf("active receipt was modified")
	}
	return nil
}

func (m Manager) activate(ctx context.Context, r *os.Root, version string) (Result, error) {
	result := Result{Version: version, Path: filepath.Join(m.Root, "versions", version, "payload")}
	_, digest, err := m.verify(ctx, r, version)
	if err != nil {
		return result, err
	}
	a, err := current(r)
	if err != nil {
		return result, err
	}
	if a.Version == version && a.ReceiptSHA256 == digest {
		result.Generation = a.Generation
		return result, nil
	}
	if a.Generation >= 10000 {
		return result, fmt.Errorf("activation history full")
	}
	txn, err := stage(r)
	if err != nil {
		return result, err
	}
	defer r.RemoveAll(txn)
	next := activation{1, version, digest, a.Generation + 1}
	if err = writeJSON(r, txn+"/activation.json", next); err != nil {
		return result, err
	}
	if err = m.boundary("before-activation"); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	// The atomic rename is the commit point. Cancellation after it cannot undo success.
	if err = r.Rename(txn+"/activation.json", fmt.Sprintf("activations/%020d.json", next.Generation)); err != nil {
		return result, err
	}
	result.Generation = next.Generation
	return result, m.boundary("committed")
}

func (m Manager) Activate(ctx context.Context, version string) (Result, error) {
	result := Result{Version: version, Path: filepath.Join(m.Root, "versions", version, "payload"), DryRun: m.DryRun}
	if err := m.validate(); err != nil {
		return result, err
	}
	if !validVersion(version) {
		return result, fmt.Errorf("invalid version")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if m.DryRun {
		return result, nil
	}
	s, err := m.open(false)
	if err != nil {
		return result, err
	}
	defer s.close()
	if err = stagingEmpty(s.root); err != nil {
		return result, err
	}
	if err = m.checkActive(ctx, s.root); err != nil {
		return result, err
	}
	return m.activate(ctx, s.root, version)
}

func (m Manager) Active(ctx context.Context) (Result, error) {
	if m.DryRun {
		return Result{DryRun: true}, nil
	}
	s, err := m.open(false)
	if err != nil {
		return Result{}, err
	}
	defer s.close()
	if err = m.checkActive(ctx, s.root); err != nil {
		return Result{}, err
	}
	a, err := current(s.root)
	if err != nil || a.Generation == 0 {
		return Result{}, err
	}
	return Result{Version: a.Version, Path: filepath.Join(m.Root, "versions", a.Version, "payload"), Generation: a.Generation}, nil
}

// Recover removes only abandoned private staging; published versions are retained.
// Caller must obtain explicit consent. Corrupt committed state blocks recovery.
func (m Manager) Recover(ctx context.Context) error {
	if err := m.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.DryRun {
		return nil
	}
	s, err := m.open(false)
	if err != nil {
		return err
	}
	defer s.close()
	if err = m.checkActive(ctx, s.root); err != nil {
		return err
	}
	entries, err := fs.ReadDir(s.root.FS(), "staging")
	if err != nil {
		return err
	}
	for _, e := range entries {
		id, ok := strings.CutPrefix(e.Name(), "txn-")
		b, err := hex.DecodeString(id)
		if !ok || err != nil || len(b) != 16 || !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unknown staging entry; recovery refused")
		}
	}
	for _, e := range entries {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = s.root.RemoveAll("staging/" + e.Name()); err != nil {
			return err
		}
	}
	return nil
}
