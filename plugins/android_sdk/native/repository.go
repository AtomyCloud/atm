package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const repositoryBase = "https://dl.google.com/android/repository/"
const repositoryURL = repositoryBase + "repository2-3.xml"
const maxMetadata = 16 << 20
const maxArchive int64 = 4 << 30
const maxExpanded int64 = 16 << 30
const maxFile int64 = 2 << 30
const maxEntries = 200000

type revision struct {
	Major   int `xml:"major"`
	Minor   int `xml:"minor,omitempty"`
	Micro   int `xml:"micro,omitempty"`
	Preview int `xml:"preview,omitempty"`
}

func (r revision) String() string { return fmt.Sprintf("%d.%d.%d", r.Major, r.Minor, r.Micro) }
func (r revision) valid() bool {
	return r.Major >= 0 && r.Minor >= 0 && r.Micro >= 0 && r.Preview >= 0 && r.Major < 100000000 && r.Minor < 100000000 && r.Micro < 100000000
}
func compare(a, b revision) int {
	aa, bb := []int{a.Major, a.Minor, a.Micro}, []int{b.Major, b.Minor, b.Micro}
	for i := range aa {
		if aa[i] < bb[i] {
			return -1
		}
		if aa[i] > bb[i] {
			return 1
		}
	}
	return 0
}

type license struct {
	ID   string `xml:"id,attr"`
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}
type reference struct {
	Ref string `xml:"ref,attr"`
}
type dependency struct {
	Path    string   `xml:"path,attr"`
	Minimum revision `xml:"min-revision"`
	Soft    bool     `xml:"soft,attr,omitempty"`
}
type details struct {
	Type    string `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	Content string `xml:",innerxml"`
}
type checksum struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}
type complete struct {
	Size     int64    `xml:"size"`
	Checksum checksum `xml:"checksum"`
	URL      string   `xml:"url"`
}
type archiveSpec struct {
	Complete complete `xml:"complete"`
	OS       string   `xml:"host-os"`
	Arch     string   `xml:"host-arch"`
	HostBits int      `xml:"host-bits"`
	JVMBits  int      `xml:"jvm-bits"`
	MinJVM   revision `xml:"min-jvm-version"`
}
type remotePackage struct {
	ID           string        `xml:"path,attr"`
	Obsolete     bool          `xml:"obsolete,attr"`
	Details      details       `xml:"type-details"`
	Revision     revision      `xml:"revision"`
	Name         string        `xml:"display-name"`
	License      reference     `xml:"uses-license"`
	Dependencies []dependency  `xml:"dependencies>dependency"`
	Channel      reference     `xml:"channelRef"`
	Archives     []archiveSpec `xml:"archives>archive"`
}
type repository struct {
	XMLName    xml.Name
	Attributes []xml.Attr      `xml:",any,attr"`
	Licenses   []license       `xml:"license"`
	Packages   []remotePackage `xml:"remotePackage"`
}
type packageSpec struct {
	ID, Directory, Name, Type, TypeContent string
	Revision                               revision
	Dependencies                           []dependency
	URL, ChecksumType, Checksum            string
	Size                                   int64
	License                                license
}
type installPlan struct {
	Schema          int
	Operation, Root string
	Packages        []packageSpec
}

func (p installPlan) digest() string {
	data, _ := json.Marshal(p)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
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
func approvedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "dl.google.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, "/android/repository/") {
		return false
	}
	name := strings.TrimPrefix(u.Path, "/android/repository/")
	return name != "" && !strings.ContainsAny(name, "/\\:") && (name == "repository2-3.xml" || strings.HasSuffix(name, ".zip"))
}
func (b *backend) get(ctx context.Context, raw string) (*http.Response, error) {
	if !approvedURL(raw) {
		return nil, fmt.Errorf("unapproved Google repository URL")
	}
	c := *b.client
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || !approvedURL(r.URL.String()) {
			return fmt.Errorf("unapproved repository redirect")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("User-Agent", "ATM/0.0.2")
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 || res.Header.Get("Content-Encoding") != "" {
		res.Body.Close()
		return nil, fmt.Errorf("repository returned status %d or encoded transfer", res.StatusCode)
	}
	return res, nil
}
func parseRepository(data []byte) (repository, error) {
	var repo repository
	if len(data) > maxMetadata || !utf8.Valid(data) {
		return repo, fmt.Errorf("invalid/oversized metadata")
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	depth, count := 0, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return repo, err
		}
		count++
		if count > 400000 {
			return repo, fmt.Errorf("metadata token limit")
		}
		switch tok.(type) {
		case xml.Directive:
			return repo, fmt.Errorf("XML directives/entities refused")
		case xml.StartElement:
			depth++
			if depth > 32 {
				return repo, fmt.Errorf("metadata nesting limit")
			}
		case xml.EndElement:
			depth--
		}
	}
	if err := xml.Unmarshal(data, &repo); err != nil {
		return repo, err
	}
	if repo.XMLName.Local != "sdk-repository" || repo.XMLName.Space != "http://schemas.android.com/sdk/android/repo/repository2/03" || len(repo.Packages) > 10000 || len(repo.Licenses) > 32 {
		return repo, fmt.Errorf("unexpected repository schema")
	}
	return repo, nil
}
func (b *backend) loadRepository(ctx context.Context) (repository, error) {
	res, err := b.get(ctx, repositoryURL)
	if err != nil {
		return repository{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, res.Body}, maxMetadata+1))
	if err != nil {
		return repository{}, err
	}
	return parseRepository(data)
}
func (r repository) resolve(id string) (packageSpec, error) {
	directory, err := packageDir(id)
	if err != nil {
		return packageSpec{}, err
	}
	var candidates []packageSpec
	for _, p := range r.Packages {
		if p.ID != id || p.Obsolete || p.Revision.Preview != 0 || (p.Channel.Ref != "channel-0" && p.Channel.Ref != "") {
			continue
		}
		if !p.Revision.valid() || p.Name == "" || len(p.Name) > 256 || len(p.Dependencies) > 32 {
			return packageSpec{}, fmt.Errorf("invalid selected package metadata")
		}
		typens := ""
		prefix, local, ok := strings.Cut(p.Details.Type, ":")
		if !ok {
			return packageSpec{}, fmt.Errorf("missing package detail type")
		}
		for _, a := range r.Attributes {
			if a.Name.Space == "xmlns" && a.Name.Local == prefix {
				typens = a.Value
			}
		}
		typ := "generic:genericDetailsType"
		if strings.HasPrefix(id, "platforms;") {
			typ = "sdk:platformDetailsType"
			if local != "platformDetailsType" || typens != "http://schemas.android.com/sdk/android/repo/repository2/03" {
				return packageSpec{}, fmt.Errorf("unsupported platform details schema")
			}
		} else if local != "genericDetailsType" || typens != "http://schemas.android.com/repository/android/generic/02" {
			return packageSpec{}, fmt.Errorf("unsupported generic details schema")
		}
		var lic license
		found := false
		for _, l := range r.Licenses {
			if l.ID == p.License.Ref {
				if found {
					return packageSpec{}, fmt.Errorf("duplicate license ID")
				}
				lic = l
				found = true
			}
		}
		if !found || lic.Type != "text" || !licenseIDRE.MatchString(lic.ID) || lic.Text == "" || len(lic.Text) > 128<<10 {
			return packageSpec{}, fmt.Errorf("missing/oversized package license")
		}
		for _, a := range p.Archives {
			if (a.OS != "" && a.OS != "linux") || (a.Arch != "" && a.Arch != "x64" && a.Arch != "x86_64" && a.Arch != "amd64") || (a.HostBits != 0 && a.HostBits != 64) || (a.JVMBits != 0 && a.JVMBits != 64) {
				continue
			}
			c := a.Complete
			hash := strings.TrimSpace(c.Checksum.Value)
			validHash := (c.Checksum.Type == "sha1" && sha1RE.MatchString(hash)) || (c.Checksum.Type == "sha256" && sha256RE.MatchString(hash))
			raw := repositoryBase + c.URL
			if !validHash || c.Size <= 0 || c.Size > maxArchive || !approvedURL(raw) {
				return packageSpec{}, fmt.Errorf("invalid package archive metadata")
			}
			if id == toolsID && (c.URL != toolsArchive || c.Size != toolsSize || c.Checksum.Type != "sha1" || hash != toolsSHA1) {
				return packageSpec{}, fmt.Errorf("pinned command-line tools metadata changed")
			}
			candidates = append(candidates, packageSpec{id, directory, p.Name, typ, p.Details.Content, p.Revision, p.Dependencies, raw, c.Checksum.Type, hash, c.Size, lic})
		}
	}
	if len(candidates) == 0 {
		return packageSpec{}, fmt.Errorf("no stable Linux amd64 archive for %s", id)
	}
	sort.Slice(candidates, func(i, j int) bool { return compare(candidates[i].Revision, candidates[j].Revision) > 0 })
	best := candidates[0]
	for _, c := range candidates[1:] {
		if compare(c.Revision, best.Revision) != 0 {
			break
		}
		a, _ := json.Marshal(best)
		bb, _ := json.Marshal(c)
		if !bytes.Equal(a, bb) {
			return packageSpec{}, fmt.Errorf("ambiguous stable archive for %s", id)
		}
	}
	return best, nil
}
func (r repository) resolvePlan(ids []string, root, operation string) (installPlan, error) {
	plan := installPlan{Schema: 1, Root: root, Operation: operation}
	state := map[string]int{}
	resolved := map[string]packageSpec{}
	var visit func(string, revision) error
	visit = func(id string, min revision) error {
		if !min.valid() || min.Preview != 0 {
			return fmt.Errorf("unsupported dependency minimum revision")
		}
		if state[id] == 1 {
			return fmt.Errorf("package dependency cycle")
		}
		p, ok := resolved[id]
		var err error
		if !ok {
			if len(resolved) >= 32 {
				return fmt.Errorf("package plan exceeds 32 packages")
			}
			p, err = r.resolve(id)
			if err != nil {
				return err
			}
			resolved[id] = p
		}
		if compare(p.Revision, min) < 0 {
			return fmt.Errorf("dependency minimum not satisfied: %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, d := range p.Dependencies {
			if err = visit(d.Path, d.Minimum); err != nil {
				return err
			}
		}
		state[id] = 2
		plan.Packages = append(plan.Packages, p)
		return nil
	}
	for _, id := range ids {
		if err := visit(id, revision{}); err != nil {
			return plan, err
		}
	}
	return plan, nil
}
func (b *backend) plan(ctx context.Context, o options, operation string) (installPlan, error) {
	ids := o.ids()
	if operation == "update" {
		records, err := b.receipts()
		if err != nil {
			return installPlan{}, err
		}
		ids = nil
		for _, r := range records {
			ids = append(ids, r.Spec.ID)
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			return installPlan{}, fmt.Errorf("no native-owned packages to update")
		}
	}
	repo, err := b.loadRepository(ctx)
	if err != nil {
		return installPlan{}, err
	}
	return repo.resolvePlan(ids, b.root, operation)
}
func licenseHash(text string) string { h := sha1.Sum([]byte(text)); return hex.EncodeToString(h[:]) }
func parseRevision(s string) (revision, error) {
	parts := strings.Split(s, ".")
	var r revision
	if len(parts) > 3 || len(parts) == 0 {
		return r, fmt.Errorf("invalid stable revision")
	}
	values := []*int{&r.Major, &r.Minor, &r.Micro}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return r, fmt.Errorf("invalid stable revision")
		}
		*values[i] = n
	}
	if !r.valid() {
		return r, fmt.Errorf("revision out of range")
	}
	return r, nil
}
