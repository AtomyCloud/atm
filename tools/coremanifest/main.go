// Prepare full-package manifests for external signing. No signing or publication.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/AtomyCloud/atm/internal/coreupdate"
	"github.com/AtomyCloud/atm/internal/install"
)

type options struct {
	archive, version, commit, goos, arch, out string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	var o options
	f := flag.NewFlagSet("coremanifest", flag.ContinueOnError)
	f.SetOutput(stderr)
	f.StringVar(&o.archive, "archive", "", "existing full runtime archive (required)")
	f.StringVar(&o.version, "version", "", "canonical MAJOR.MINOR.PATCH (required)")
	f.StringVar(&o.commit, "commit", "", "clean lowercase 40-hex source commit (required)")
	f.StringVar(&o.goos, "os", "", "linux, darwin or windows (required)")
	f.StringVar(&o.arch, "arch", "", "amd64 or arm64 (required)")
	f.StringVar(&o.out, "out", "", "new output directory; parent must exist (required)")
	f.Usage = func() {
		fmt.Fprintln(stderr, "Usage: coremanifest --archive FILE --version VERSION --commit COMMIT --os OS --arch ARCH --out NEW_DIR")
		f.PrintDefaults()
		fmt.Fprintln(stderr, "Creates manifest.json and manifest.signing-input for external raw Ed25519 signing.")
		fmt.Fprintln(stderr, "No private keys, signing, network access, publication or installation.")
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	return prepare(ctx, o)
}

func (o options) manifest() (coreupdate.PackageManifest, string, error) {
	root := "atm-" + o.version + "-" + o.goos + "-" + o.arch
	ext := ".tar.gz"
	if o.goos == "windows" {
		ext = ".zip"
	}
	// Validate the supplied identity with the shared policy before any file I/O.
	m := coreupdate.PackageManifest{
		Schema: 1, Scope: coreupdate.PackageScope,
		Source:  coreupdate.OfficialSource + "/releases/download/v" + o.version + "/" + root + ext,
		Version: o.version, Commit: o.commit, OS: o.goos, Arch: o.arch, Migration: true,
		Archive:       install.File{SHA256: strings.Repeat("0", 64), Size: 1, Mode: 0644},
		ReleaseSHA256: strings.Repeat("0", 64),
	}
	if o.archive == "" || o.out == "" {
		return m, root, fmt.Errorf("--archive and --out are required")
	}
	return m, root, m.Validate()
}

func prepare(ctx context.Context, o options) error {
	m, root, err := o.manifest()
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	input, info, err := openRegular(o.archive, coreupdate.MaxPackageBytes)
	if err != nil {
		return err
	}
	defer input.Close()
	// Hash and inspect one private snapshot, never two reads of a mutable input.
	temp, err := os.MkdirTemp("", "atm-coremanifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	// macOS commonly exposes its temporary directory through /var's symlink.
	// Resolve our newly created directory before the inspector's no-link checks.
	temp, err = filepath.EvalSymlinks(temp)
	if err != nil {
		return err
	}
	path := filepath.Join(temp, filepath.Base(m.Source))
	snapshot, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	defer snapshot.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(snapshot, h), io.LimitReader(contextReader{ctx, input}, coreupdate.MaxPackageBytes+1))
	if err != nil {
		return err
	}
	after, err := input.Stat()
	if err != nil {
		return err
	}
	if n != info.Size() || n > coreupdate.MaxPackageBytes || after.Size() != info.Size() ||
		!after.ModTime().Equal(info.ModTime()) || after.Mode() != info.Mode() {
		return fmt.Errorf("archive changed while reading or exceeds size limit")
	}
	m.Archive.SHA256, m.Archive.Size = fmt.Sprintf("%x", h.Sum(nil)), n
	release, err := readRelease(ctx, snapshot, n, root, o.goos == "windows")
	if err != nil {
		return err
	}
	m.ReleaseSHA256 = fmt.Sprintf("%x", sha256.Sum256(release))
	if err = snapshot.Close(); err != nil {
		return err
	}
	if err = m.Validate(); err != nil {
		return err
	}
	if _, err = coreupdate.InspectPackage(ctx, path, m); err != nil {
		return fmt.Errorf("invalid runtime package: %w", err)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > coreupdate.MaxManifestBytes {
		return fmt.Errorf("manifest exceeds size limit")
	}
	signing := append([]byte(coreupdate.PackageSignatureDomain), raw...)
	return writeOutputs(ctx, o.out, []artifact{{"manifest.json", raw}, {"manifest.signing-input", signing}})
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
