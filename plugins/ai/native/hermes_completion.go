//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const hermesCompletionMetadataLimit = int64(16 << 20)

type hermesCompletionFile struct {
	Path, SHA256 string
	Size         int64
	Mode         uint32
}

// This records preparation evidence, not a runtime or whole-environment attestation.
type hermesCompletionEvidence struct {
	Schema                                       int
	Source, InstallKey, Environment, StorePython string
	PythonResolved, PythonLink                   string
	StartedAt, VerifiedAt                        time.Time
	Files                                        []hermesCompletionFile
}

// Schemas: pinned pm/receipt.py:132,227 and pm/lock.py:231,269.
type hermesPMSyncReceipt struct {
	Schema      int             `json:"schema"`
	Kind        string          `json:"kind"`
	Outcome     string          `json:"outcome"`
	ExitCode    *int            `json:"exit_code"`
	StartedAt   string          `json:"started_at"`
	FinishedAt  string          `json:"finished_at"`
	FeatureList []string        `json:"feature_list"`
	Refusal     json.RawMessage `json:"refusal"`
	VenvRebuild *struct {
		OK *bool `json:"ok"`
	} `json:"venv_rebuild"`
}

type hermesPMVenvFact struct {
	Environment  string   `json:"environment"`
	ResolvedLock string   `json:"resolved_lock"`
	Stamp        string   `json:"stamp"`
	Extras       []string `json:"extras"`
}

// Read only regular, singly-linked owned files; preserve hashes and modes for later checks.
func hermesCompletionRead(ctx context.Context, root, path string, limit int64, executable, capture bool) ([]byte, hermesCompletionFile, error) {
	var evidence hermesCompletionFile
	if err := ctx.Err(); err != nil {
		return nil, evidence, err
	}
	if err := absolutePath(path); err != nil {
		return nil, evidence, err
	}
	if !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return nil, evidence, fmt.Errorf("completion file escapes version root")
	}
	i, err := os.Lstat(path)
	if err != nil {
		return nil, evidence, err
	}
	if i.Size() <= 0 || i.Mode().Perm()&0022 != 0 || (executable && i.Mode().Perm()&0111 == 0) {
		return nil, evidence, fmt.Errorf("invalid completion file permissions or size: %s", path)
	}
	f, err := regular(path, i.Mode().Perm(), limit)
	if err != nil {
		return nil, evidence, err
	}
	defer f.Close()
	h := sha256.New()
	var body bytes.Buffer
	var out io.Writer = h
	if capture {
		out = io.MultiWriter(h, &body)
	}
	n, err := io.Copy(out, io.LimitReader(contextReader{ctx, f}, limit+1))
	if err != nil {
		return nil, evidence, err
	}
	j, statErr := f.Stat()
	k, pathErr := os.Lstat(path)
	if statErr != nil || pathErr != nil || n != i.Size() || n > limit || j.Size() != n || k.Size() != n || !os.SameFile(i, j) || !os.SameFile(i, k) || j.Mode() != i.Mode() || k.Mode() != i.Mode() || !j.ModTime().Equal(i.ModTime()) || !k.ModTime().Equal(i.ModTime()) || !singleLink(j) || !singleLink(k) || owner(j) != uint32(os.Geteuid()) || owner(k) != uint32(os.Geteuid()) {
		return nil, evidence, fmt.Errorf("completion file changed while reading: %s", path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, evidence, err
	}
	evidence = hermesCompletionFile{filepath.ToSlash(rel), hex.EncodeToString(h.Sum(nil)), n, uint32(i.Mode().Perm())}
	return body.Bytes(), evidence, ctx.Err()
}

func hermesCompletionSnapshot(ctx context.Context, versionDir string, startedAt, verifiedAt time.Time, launchers bool) (hermesCompletionEvidence, error) {
	e := hermesCompletionEvidence{Schema: 1, StartedAt: startedAt.UTC(), VerifiedAt: verifiedAt.UTC()}
	if startedAt.IsZero() || verifiedAt.Before(startedAt) {
		return e, fmt.Errorf("PM attempt start time is required")
	}
	if err := dirs(versionDir, false, true); err != nil {
		return e, err
	}
	e.Source = filepath.Join(versionDir, "source")
	if err := dirs(e.Source, false, true); err != nil {
		return e, err
	}
	key := sha256.Sum256([]byte(e.Source)) // dirs rejects symlinked ancestors: this is the canonical path.
	e.InstallKey = hex.EncodeToString(key[:])[:16]
	home := filepath.Join(versionDir, "home", ".hermes")
	state := filepath.Join(home, "installs", e.InstallKey)
	read := func(path string, limit int64, executable, capture bool) ([]byte, error) {
		data, file, err := hermesCompletionRead(ctx, versionDir, path, limit, executable, capture)
		if err == nil {
			e.Files = append(e.Files, file)
		}
		return data, err
	}
	data, err := read(filepath.Join(versionDir, "source.json"), hermesCompletionMetadataLimit, false, true)
	if err != nil {
		return e, err
	}
	var record hermesRecord
	if err := decode(data, &record, true); err != nil {
		return e, err
	}
	if record.Schema != 1 || record.Plugin != "ai" || record.Provider != hermesAgent || record.Source.Version != hermesPMVersion || record.Source.Commit != hermesPMCommit || (record.Arch != "amd64" && record.Arch != "arm64") {
		return e, fmt.Errorf("completion source identity is not the reviewed contract")
	}
	var sourceLock []byte
	for _, name := range []string{"pyproject.toml", "uv.lock", "pm/lock.json", "hermes_cli/venv_sync.py", "hermes_cli/_launchers.py"} {
		data, err = read(filepath.Join(e.Source, filepath.FromSlash(name)), hermesCompletionMetadataLimit, false, true)
		if err != nil {
			return e, err
		}
		original, ok := record.Source.Files[name]
		if !ok || original.Size != int64(len(data)) || original.SHA != hermesObjectHash("blob", data) {
			return e, fmt.Errorf("completion input differs from pinned source: %s", name)
		}
		if name == "pm/lock.json" {
			sourceLock = data
		}
	}
	data, err = read(filepath.Join(home, "logs", "update_receipts", "latest.json"), 1<<20, false, true)
	if err != nil {
		return e, err
	}
	var receipt hermesPMSyncReceipt
	if err := decode(data, &receipt, false); err != nil {
		return e, err
	}
	start, startErr := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	finish, finishErr := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	// The caller guarantees a new attempt directory and private HOME. receipt.py
	// uses datetime.isoformat (microseconds). install.py:785 records
	// rebuild.ok=false for an already-current environment, not a failed sync.
	if receipt.Schema != 1 || receipt.Kind != "sync" || receipt.Outcome != "ok" || receipt.ExitCode == nil || *receipt.ExitCode != 0 || receipt.VenvRebuild == nil || receipt.VenvRebuild.OK == nil || (len(receipt.Refusal) != 0 && string(receipt.Refusal) != "null") || startErr != nil || finishErr != nil || start.Before(startedAt.Truncate(time.Microsecond)) || finish.Before(start) || finish.After(verifiedAt) {
		return e, fmt.Errorf("PM sync receipt is missing successful fresh completion")
	}
	data, err = read(filepath.Join(state, "facts.json"), 1<<20, false, true)
	if err != nil {
		return e, err
	}
	var facts struct {
		Schema   int `json:"schema"`
		Packages struct {
			Venv hermesPMVenvFact `json:"venv"`
		} `json:"packages"`
	}
	if err := decode(data, &facts, false); err != nil {
		return e, err
	}
	venv := facts.Packages.Venv
	if facts.Schema != 1 || !validSHA(venv.Stamp) || !reflect.DeepEqual(venv.Extras, []string{"all"}) || !reflect.DeepEqual(receipt.FeatureList, venv.Extras) {
		return e, fmt.Errorf("unexpected PM environment stamp or feature selection")
	}
	e.Environment = venv.Environment
	generation := filepath.Dir(e.Environment)
	id := filepath.Base(generation)
	decodedID, idErr := hex.DecodeString(id)
	if absolutePath(e.Environment) != nil || filepath.Base(e.Environment) != "venv" || filepath.Dir(generation) != filepath.Join(state, "environments") || idErr != nil || len(decodedID) != 16 || strings.ToLower(id) != id || venv.ResolvedLock != filepath.Join(generation, "workspace", "uv.lock") {
		return e, fmt.Errorf("PM environment or resolved lock is outside this source installation")
	}
	config, err := read(filepath.Join(e.Environment, "pyvenv.cfg"), 64<<10, false, true)
	if err != nil {
		return e, err
	}
	if _, err := read(venv.ResolvedLock, hermesCompletionMetadataLimit, false, false); err != nil {
		return e, err
	}
	data, err = read(filepath.Join(state, "declined-packages.json"), 64<<10, false, true)
	if err != nil {
		return e, err
	}
	var declined struct {
		Schema   int      `json:"schema"`
		Declined []string `json:"declined"`
	}
	if err := decode(data, &declined, true); err != nil {
		return e, err
	}
	if declined.Schema != 1 || !reflect.DeepEqual(declined.Declined, []string{"agent-browser", "cua-driver"}) {
		return e, fmt.Errorf("PM browser/computer-use exclusions are not recorded")
	}
	var pins struct {
		Packages struct {
			Python struct {
				Version   string               `json:"version"`
				Artifacts map[string]hermesPin `json:"artifacts"`
			} `json:"python"`
		} `json:"packages"`
	}
	if err := decode(sourceLock, &pins, false); err != nil {
		return e, err
	}
	data, err = read(filepath.Join(home, "tools", "facts.json"), 1<<20, false, true)
	if err != nil {
		return e, err
	}
	var tools struct {
		Schema   int `json:"schema"`
		Packages struct {
			Python struct {
				Entry     string   `json:"entry"`
				Version   string   `json:"version"`
				Target    string   `json:"target"`
				Artifacts []string `json:"artifacts"`
				Digest    string   `json:"digest"`
			} `json:"python"`
		} `json:"packages"`
	}
	if err := decode(data, &tools, false); err != nil {
		return e, err
	}
	python := tools.Packages.Python
	target := "linux-x64"
	if record.Arch == "arm64" {
		target = "linux-arm64"
	}
	pin := pins.Packages.Python
	if tools.Schema != 1 || pin.Version != "3.14.9+20261021" || python.Version != pin.Version || python.Target != target || python.Entry != "python-"+pin.Version+"-"+target || !validSHA(python.Digest) || !validSHA(pin.Artifacts[target].SHA256) || !reflect.DeepEqual(python.Artifacts, []string{pin.Artifacts[target].SHA256}) {
		return e, fmt.Errorf("PM store interpreter does not match source pin")
	}
	entry := filepath.Join(home, "tools", python.Entry)
	e.StorePython = filepath.Join(entry, "bin", "python3")
	link, err := os.Lstat(e.StorePython)
	if err != nil {
		return e, err
	}
	if owner(link) != uint32(os.Geteuid()) || !singleLink(link) {
		return e, fmt.Errorf("unowned PM interpreter")
	}
	if link.Mode()&os.ModeSymlink != 0 {
		e.PythonLink, err = os.Readlink(e.StorePython)
		if err != nil {
			return e, err
		}
	}
	if err := hermesOwnedExecutable(entry, e.StorePython); err != nil {
		return e, err
	}
	if err := dirs(filepath.Dir(e.StorePython), false, false); err != nil {
		return e, err
	}
	e.PythonResolved, err = filepath.EvalSymlinks(e.StorePython)
	if err != nil {
		return e, err
	}
	if _, err := read(e.PythonResolved, maxBinary, true, false); err != nil {
		return e, err
	}
	if err := hermesCompletionPyvenv(config, filepath.Dir(e.PythonResolved)); err != nil {
		return e, err
	}
	if err := dirs(filepath.Join(e.Environment, "lib", "python3.14", "site-packages"), false, false); err != nil {
		return e, err
	}
	if launchers {
		for _, name := range []string{"hermes", "hermes-acp"} {
			if _, err := read(filepath.Join(e.Source, ".hermes", "bin", name), 256<<10, true, false); err != nil {
				return e, err
			}
		}
	}
	return e, ctx.Err()
}

// uv 0.13.5 crates/uv-virtualenv/src/virtualenv.rs:542 writes this key/value format.
func hermesCompletionPyvenv(data []byte, pythonHome string) error {
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" {
			return fmt.Errorf("invalid pyvenv.cfg")
		}
		if _, exists := values[key]; exists {
			return fmt.Errorf("duplicate pyvenv.cfg field")
		}
		values[key] = value
	}
	if values["home"] != pythonHome || values["implementation"] != "CPython" || values["version_info"] != "3.14.9" || values["include-system-site-packages"] != "false" || values["relocatable"] != "true" || values["uv"] != "0.13.5" {
		return fmt.Errorf("pyvenv.cfg does not bind the pinned private interpreter")
	}
	return nil
}

func (b *backend) hermesCompletePM(ctx context.Context, versionDir, bootstrapPython string, startedAt time.Time) (hermesCompletionEvidence, error) {
	var empty hermesCompletionEvidence
	before, err := hermesCompletionSnapshot(ctx, versionDir, startedAt, time.Now().UTC(), false)
	if err != nil {
		return empty, err
	}
	if err := hermesOwnedExecutable(filepath.Join(versionDir, "python"), bootstrapPython); err != nil {
		return empty, err
	}
	env := hermesEnvironment(versionDir)
	out, err := b.hermesRun(ctx, bootstrapPython, []string{"-s", "-B", "-m", "hermes_cli.venv_sync", "--project-root", before.Source, "--check", "--json"}, before.Source, env)
	if err != nil {
		return empty, err
	}
	var verdict struct {
		State string `json:"state"`
		OK    *bool  `json:"ok"`
	}
	if len(out) > 4096 {
		return empty, fmt.Errorf("PM currency output exceeds limit")
	}
	if err := decode([]byte(out), &verdict, true); err != nil {
		return empty, err
	}
	if verdict.State != "current" || verdict.OK == nil || !*verdict.OK {
		return empty, fmt.Errorf("PM did not confirm the current dependency environment")
	}
	check, err := hermesCompletionSnapshot(ctx, versionDir, startedAt, before.VerifiedAt, false)
	if err != nil {
		return empty, err
	}
	if !reflect.DeepEqual(before, check) {
		return empty, fmt.Errorf("PM completion inputs changed during currency check")
	}
	bin := filepath.Join(before.Source, ".hermes", "bin")
	out, err = b.hermesRun(ctx, bootstrapPython, []string{"-I", "-B", filepath.Join(before.Source, "hermes_cli", "_launchers.py"), bin}, before.Source, env)
	if err != nil {
		return empty, err
	}
	if strings.TrimSpace(out) != filepath.Join(bin, "hermes")+"\n"+filepath.Join(bin, "hermes-acp") {
		return empty, fmt.Errorf("unexpected local launcher publication result")
	}
	after, err := hermesCompletionSnapshot(ctx, versionDir, startedAt, before.VerifiedAt, true)
	if err != nil {
		return empty, err
	}
	base := after
	base.Files = base.Files[:len(base.Files)-2]
	if !reflect.DeepEqual(before, base) {
		return empty, fmt.Errorf("PM completion inputs changed during launcher publication")
	}
	after.VerifiedAt = time.Now().UTC()
	return after, nil
}

// Later status only compares recorded preparation evidence; it never starts PM or repairs state.
func verifyHermesCompletion(ctx context.Context, versionDir string, evidence hermesCompletionEvidence) error {
	if evidence.Schema != 1 || evidence.VerifiedAt.After(time.Now()) {
		return fmt.Errorf("invalid Hermes completion evidence")
	}
	current, err := hermesCompletionSnapshot(ctx, versionDir, evidence.StartedAt, evidence.VerifiedAt, true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, evidence) {
		return fmt.Errorf("Hermes completion evidence changed; state preserved")
	}
	return ctx.Err()
}

// pm workspace snapshots live outside source. This reviewed Linux preparation
// path writes only the two _launchers.py outputs, not .venv links or build trees.
func verifyHermesGeneratedSource(ctx context.Context, source hermesSource, versionDir string) error {
	trees, err := source.validate(ctx)
	if err != nil {
		return err
	}
	root := filepath.Join(versionDir, "source")
	if err := dirs(root, false, true); err != nil {
		return err
	}
	count := 0
	var walk func(string) error
	walk = func(rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		directory := filepath.Join(root, filepath.FromSlash(rel))
		if err := dirs(directory, false, false); err != nil {
			return err
		}
		f, err := os.Open(directory)
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			batch, readErr := f.ReadDir(128)
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			for _, item := range batch {
				if err := ctx.Err(); err != nil {
					return err
				}
				count++
				if count > len(source.Files)+len(trees)+4 {
					return fmt.Errorf("generated source entry bound exceeded")
				}
				name := strings.TrimPrefix(filepath.ToSlash(filepath.Join(rel, item.Name())), "./")
				if _, tracked := source.Files[name]; tracked {
					continue
				} // Caller verifies tracked bytes separately.
				if trees[name] != "" || name == ".hermes" || name == ".hermes/bin" {
					if !item.IsDir() || item.Type()&os.ModeSymlink != 0 {
						return fmt.Errorf("unsafe generated source directory: %s", name)
					}
					if err := walk(name); err != nil {
						return err
					}
					continue
				}
				if name != ".hermes/bin/hermes" && name != ".hermes/bin/hermes-acp" {
					return fmt.Errorf("unapproved generated source entry: %s", name)
				}
				if _, _, err := hermesCompletionRead(ctx, versionDir, filepath.Join(root, filepath.FromSlash(name)), 256<<10, true, false); err != nil {
					return err
				}
			}
			if readErr == io.EOF {
				return nil
			}
		}
	}
	return walk(".")
}
