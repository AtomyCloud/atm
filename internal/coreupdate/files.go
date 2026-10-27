package coreupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/AtomyCloud/atm/internal/install"
)

const transaction = ".atm-coreupdate"
const lockName = ".atm-coreupdate.lock"

type store struct {
	root *os.Root
	lock *os.File
}

func (st *store) close() { st.lock.Close(); st.root.Close() }

// Rooted operations confine writes; the caller must also exclude external
// writers. This is not isolation against a hostile process with the same UID.
func openRoot(path string) (*os.Root, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return nil, fmt.Errorf("invalid installation root")
	}
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("linked/non-directory root refused")
		}
		if p == path && runtime.GOOS != "windows" && info.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("installation root writable by other users")
		}
		gitPath := filepath.Join(p, ".git")
		gitInfo, gitErr := os.Lstat(gitPath)
		if gitErr == nil {
			// The execution sandbox may place empty .git placeholders in
			// ancestors such as /tmp. A direct marker is always refused.
			if p == path || !gitInfo.IsDir() || gitInfo.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("checkout ownership boundary refused")
			}
			f, err := os.Open(gitPath)
			if err != nil {
				return nil, err
			}
			entries, err := f.ReadDir(1)
			f.Close()
			if err != io.EOF || len(entries) != 0 {
				return nil, fmt.Errorf("checkout or unreadable ownership boundary refused")
			}
		} else if !errors.Is(gitErr, os.ErrNotExist) {
			return nil, gitErr
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return os.OpenRoot(path)
}

func (s *Service) open(createLock bool) (*store, error) {
	r, err := openRoot(s.p.Root)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*store, error) { r.Close(); return nil, err }
	flags := os.O_RDWR
	if createLock {
		f, err := r.OpenFile(lockName, flags|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			f.Close()
		} else if !errors.Is(err, os.ErrExist) {
			return fail(err)
		}
	}
	f, err := regular(r, lockName, flags, 0)
	if err != nil {
		return fail(err)
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return fail(fmt.Errorf("%w: %v", ErrBusy, err))
	}
	return &store{r, f}, nil
}

func regular(r *os.Root, name string, flags int, max int64) (*os.File, error) {
	info, err := r.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() < 0 || info.Size() > max {
		return nil, fmt.Errorf("unsafe or oversized file: %s", name)
	}
	f, err := r.OpenFile(name, flags, 0)
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err == nil && (!os.SameFile(info, actual) || !actual.Mode().IsRegular() || actual.Size() > max) {
		err = fmt.Errorf("file changed while opening: %s", name)
	}
	if err == nil {
		err = singleLink(f, actual)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func writeFile(ctx context.Context, r *os.Root, name string, src io.Reader, max int64, mode os.FileMode) (install.File, error) {
	var got install.File
	f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return got, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, src}, max+1))
	if err == nil && n > max {
		err = fmt.Errorf("payload size limit exceeded")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return install.File{SHA256: hex.EncodeToString(h.Sum(nil)), Size: n, Mode: uint32(mode)}, err
}

func fingerprint(ctx context.Context, r *os.Root, name string, max int64) (install.File, error) {
	var got install.File
	f, err := regular(r, name, os.O_RDONLY, max)
	if err != nil {
		return got, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return got, err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, max+1))
	if err != nil {
		return got, err
	}
	if n > max || n != info.Size() {
		return got, fmt.Errorf("file size changed")
	}
	return install.File{SHA256: hex.EncodeToString(h.Sum(nil)), Size: n, Mode: uint32(info.Mode().Perm())}, ctx.Err()
}

func match(ctx context.Context, r *os.Root, name string, want install.File) error {
	got, err := fingerprint(ctx, r, name, want.Size)
	if err != nil {
		return err
	}
	if got.SHA256 != want.SHA256 || got.Size != want.Size || runtime.GOOS != "windows" && got.Mode != want.Mode {
		return fmt.Errorf("modified file: %s", name)
	}
	return nil
}

func readFile(r *os.Root, name string, max int64) ([]byte, error) {
	f, err := regular(r, name, os.O_RDONLY, max)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(b)) > max {
		return nil, fmt.Errorf("state size limit exceeded")
	}
	return b, err
}

// StartupGuard is a read-only, trust-independent conservative guard for callers
// which have no configured key. Any transaction blocks normal execution. A
// configured Service.CheckStartup can verify completed/rolled-back transactions.
// Callers must invoke the guard before normal services.
func StartupGuard(root string) error {
	// Normal startup may use a development checkout or a shared read-only
	// installation. Mutation-only ownership checks belong to Service.open.
	if !filepath.IsAbs(root) {
		return fmt.Errorf("absolute startup root required")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = r.Lstat(transaction)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrRecoveryRequired
}
