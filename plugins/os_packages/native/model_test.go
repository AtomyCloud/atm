package main

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestModelDetectLegacyFamilies(t *testing.T) {
	for _, tc := range []struct {
		id, manager, name string
		ubuntu            bool
	}{
		{"ubuntu", "apt-get", "ubuntu", true}, {"zorin", "apt-get", "zorin", true},
		{"debian", "apt-get", "debian", false}, {"centos", "yum", "centos", false},
		{"rocky", "dnf", "rockylinux", false}, {"rockylinux", "dnf", "rockylinux", false},
		{"almalinux", "dnf", "almalinux", false}, {"fedora", "dnf", "fedora", false},
		{"rhel", "dnf", "rhel", false}, {"arch", "pacman", "archlinux", false},
		{"archlinux", "pacman", "archlinux", false}, {"alpine", "apk", "alpine", false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			s, err := detectSystem([]byte("ID="+tc.id+"\nVERSION_CODENAME=noble\n"), true)
			want := systemInfo{ID: tc.id, Name: tc.name, Manager: tc.manager, Codename: "noble", Ubuntu: tc.ubuntu}
			if err != nil || s != want {
				t.Fatalf("got %#v, %v; want %#v", s, err, want)
			}
		})
	}
}

func TestModelDetectStructuredMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, input, manager, id, codename, display string
		marker                                      bool
	}{
		{"quoted", "# vendor\r\nID=\"zorin\"\r\nNAME='Zorin OS'\r\nVERSION_CODENAME=zorin\r\nUBUNTU_CODENAME=jammy\r\n", "apt-get", "zorin", "jammy", "Zorin OS", false},
		{"derivative", "ID=custom\nID_LIKE=\"other ubuntu debian\"", "apt-get", "custom", "", "custom", false},
		{"rpm derivative", "ID=custom\nID_LIKE='rhel fedora'", "dnf", "custom", "", "custom", false},
		{"ordered like", "ID=custom\nID_LIKE='fedora debian'", "dnf", "custom", "", "custom", false},
		{"empty marker", "", "pacman", "", "", "archlinux", true},
		{"unknown marker", "ID=custom\nID_LIKE=debian", "pacman", "custom", "", "archlinux", true},
		{"like without id", "ID_LIKE=debian", "apt-get", "", "", "debian-like", false},
		{"rpm without id", "ID_LIKE=rhel", "dnf", "", "", "rhel-like", false},
		{"escaped literal", "ID=ubuntu\nNAME=\"Ubuntu \\\"Desktop\\\"\"", "apt-get", "ubuntu", "", "Ubuntu \"Desktop\"", false},
		{"no codename guess", "ID=ubuntu\nVERSION_ID=24.04", "apt-get", "ubuntu", "", "ubuntu", false},
		{"empty preferred", "ID=zorin\nUBUNTU_CODENAME=\nVERSION_CODENAME=noble", "apt-get", "zorin", "", "zorin", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := detectSystem([]byte(tc.input), tc.marker)
			if err != nil || s.ID != tc.id || s.Manager != tc.manager || s.Codename != tc.codename || s.Name != tc.display {
				t.Fatalf("unexpected detection: %#v, %v", s, err)
			}
			if s.Ubuntu != (tc.id == "ubuntu" || tc.id == "zorin") {
				t.Fatal("Ubuntu sources eligibility inferred from ID_LIKE or marker")
			}
		})
	}
}

func TestModelDetectCodenameDoesNotRestrictConfiguredRepositories(t *testing.T) {
	for _, tc := range []struct{ id, codename, manager string }{
		{"debian", "sid", "apt-get"},
		{"debian", "testing", "apt-get"},
		{"ubuntu", "devel", "apt-get"},
		{"arch", "rolling", "pacman"},
		{"fedora", "CustomRelease", "dnf"},
		{"ubuntu", "noble-security", "apt-get"},
		{"ubuntu", "noble/../../x", "apt-get"},
		{"ubuntu", "stable", "apt-get"},
		{"ubuntu", "$(id)", "apt-get"},
		{"ubuntu", "24.04", "apt-get"},
		{"ubuntu", "noble extra", "apt-get"},
		{"ubuntu", "Noble", "apt-get"},
	} {
		t.Run(tc.id+"/"+tc.codename, func(t *testing.T) {
			input := "ID=" + tc.id + "\nVERSION_CODENAME='" + tc.codename + "'\n"
			s, err := detectSystem([]byte(input), false)
			if err != nil || s.ID != tc.id || s.Manager != tc.manager || s.Codename != tc.codename {
				t.Fatalf("codename blocked detection: %#v, %v", s, err)
			}
			got, err := planCommands(s, []string{"curl"}, "install", "")
			withoutCodename := s
			withoutCodename.Codename = ""
			want, wantErr := planCommands(withoutCodename, []string{"curl"}, "install", "")
			if err != nil || wantErr != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("inert codename affected configured-repository plan: %v, %v", got, err)
			}
			for _, arch := range []string{"amd64", "arm64"} {
				if sources, err := ubuntuSources(s, arch); err == nil || sources != "" {
					t.Fatalf("accepted unreviewed sources: %q, %v", sources, err)
				}
			}
			if _, err := planCommands(s, []string{"curl"}, "install", "/tmp/sources.list"); err == nil {
				t.Fatal("temporary-source plan accepted unreviewed suite")
			}
		})
	}
}

func TestModelDetectRejectsMalformedAndUnsupported(t *testing.T) {
	inputs := []string{
		"", "ID=freebsd", "ID=opensuse", "ID=custom\nID_LIKE=notdebian",
		"ID=custom\nID_LIKE=ubuntuevil", "ID=custom\nID_LIKE=prefedora",
		"ID=custom\nID_LIKE='rhel;ubuntu'", "ID=Ubuntu", "ID='ubuntu debian'",
		"ID=ubuntu\nID=debian", "ID=ubuntu;", "ID=ubuntu NAME=Ubuntu", "export ID=ubuntu",
		"ID=ubuntu\necho injected", "ID=$(id)", "ID=ubuntu\nNAME=\"$(id)\"",
		"ID=ubuntu\nNAME=\"`id`\"", "ID=ubuntu\nNAME=\"${HOME}\"", "ID=ubuntu\nNAME=$'text'",
		"ID=ubuntu\nNAME='a'\"b\"", "ID=ubuntu\nNAME=\"unfinished", "ID=ubuntu\nNAME=(array)",
		"ID=ubuntu\nNAME+=x", "ID=ubuntu > output", "ID=ubuntu &", "ID=ubuntu || true",
		"ID=ubuntu\nUBUNTU_CODENAME=$(id)", "ID=ubuntu\nVERSION_CODENAME=\"${HOME}\"",
		"ID=ubuntu\nUBUNTU_CODENAME='noble\tbad'", "ID=ubuntu\nVERSION_CODENAME='noble\x1b'",
		"ID=ubuntu\nVERSION_CODENAME='noble\u202e'",
		"ID=ubuntu\x00", "ID=ubuntu\rNAME=bad", "ID=ubuntu\nNAME='a\tb'", "ID=ubuntu\xff",
		"ID=ubuntu\nNAME='hidden\u202e'", "ID=ubuntu\n#\x1b[31m",
		"ID=ubuntu\n#" + strings.Repeat("x", maxModelLineBytes), strings.Repeat("\n", maxOSReleaseBytes+1),
	}
	for i, input := range inputs {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if s, err := detectSystem([]byte(input), false); err == nil || s != (systemInfo{}) {
				t.Fatalf("accepted malformed/unsupported input %q: %#v, %v", input, s, err)
			}
		})
	}
	if _, err := detectSystem([]byte("ID=$(id)"), true); err == nil {
		t.Fatal("arch marker bypassed malformed metadata")
	}
}

func TestModelRealPackageFile(t *testing.T) {
	raw, err := os.ReadFile("../packages.txt")
	if err != nil {
		t.Fatal(err)
	}
	for manager, specific := range map[string]string{"apt-get": "netcat-openbsd", "dnf": "nmap-ncat", "yum": "lksctp-tools", "pacman": "python-pipx", "apk": "py3-pip"} {
		t.Run(manager, func(t *testing.T) {
			packages, err := parsePackages(raw, manager)
			if err != nil || len(packages) < 20 || !slices.Contains(packages, specific) || !slices.Contains(packages, "curl") {
				t.Fatalf("real package fixture: %v, %v", packages, err)
			}
			for _, action := range []string{"install", "remove", "uninstall"} {
				if _, err := planCommands(systemInfo{Manager: manager}, packages, action, ""); err != nil {
					t.Fatalf("real fixture plan %s: %v", action, err)
				}
			}
		})
	}
}

func TestModelPackageCommentsAndDedup(t *testing.T) {
	raw := []byte(" # comment\r\n[apt-get] # selected\r\n curl # first\r\ngit\r\ncurl\r\ngit\t# duplicate\r\n\r\n[apk]\r\npy3-pip\r\n")
	got, err := parsePackages(raw, "apt-get")
	if err != nil || !reflect.DeepEqual(got, []string{"curl", "git"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for manager, names := range map[string][]string{
		"apt-get": {"libstdc++6", "python3-pip", "libc6:amd64"},
		"dnf":     {"NetworkManager", "libstdc++", "python3.11", "foo_bar"},
		"yum":     {"NetworkManager", "libstdc++", "foo_bar"},
		"pacman":  {"libstdc++5", "foo@bar", "foo_bar"},
		"apk":     {"g++", "py3-pip", "foo_bar"},
	} {
		got, err := parsePackages([]byte("["+manager+"]\n"+strings.Join(names, "\n")), manager)
		if err != nil || !reflect.DeepEqual(got, names) {
			t.Fatalf("valid %s package names: %v, %v", manager, got, err)
		}
	}
}

func TestModelPackagesRejectMalformed(t *testing.T) {
	for _, raw := range []string{
		"", "curl", "[apt-get]", "[unknown]\ncurl", "[apt-get]\ncurl\n[apt-get]\ngit",
		"[ apt-get ]\ncurl", "[APT-GET]\ncurl", "[apt-get\ncurl", "[apt-get]]\ncurl",
		"[apt-get] trailing\ncurl", "[apt-get]\ncurl git", "[apt-get]\ncurl\tgit",
		"[apt-get]\ncurl#comment", "[apt-get]\n; comment", "[apt-get]\ncurl\n[apk]\n--evil",
		"[apt-get]\ncurl\n[apk]\nNAME", "[apt-get]\ncurl\n[unknown]\nokay",
		"[apt-get]\ncurl\rname", "[apt-get]\ncurl\x00", "[apt-get]\ncurl\xff",
		"[apt-get]\n#\x1b", "[apt-get]\ncurl\n#" + strings.Repeat("a", maxModelLineBytes),
		strings.Repeat("\n", maxPackageFileBytes+1),
	} {
		if got, err := parsePackages([]byte(raw), "apt-get"); err == nil || got != nil {
			t.Fatalf("accepted malformed package file %q: %v, %v", raw, got, err)
		}
	}
	if _, err := parsePackages([]byte("[apk]\ncurl"), "apt-get"); err == nil {
		t.Fatal("accepted missing selected section")
	}
	if _, err := parsePackages([]byte("[apt-get]\ncurl"), "apt"); err == nil {
		t.Fatal("accepted unsupported manager")
	}
}

func TestModelPackageInjectionRejectedEverywhere(t *testing.T) {
	bad := []string{"--help", "-y", "-", "*", "foo*", "foo?", "foo[ab]", "foo{a,b}", "https://host/pkg", "../pkg", "/tmp/pkg", "foo/bar", "./pkg", "$(id)", "`id`", "${HOME}", "foo;id", "foo|id", "foo&&id", "foo>file", "foo<file", "foo\nbar", "foo\rbar", "foo\tbar", "foo bar", "foo\x00", "foo\x7f", "foo\u00a0bar", "foo\u202ebar", "'curl'", "\"curl\"", "foo=1", "foo>=1", "foo~1", "!foo", "foo\\bar", ".", "..", "foo.rpm", "foo.deb", "foo.apk", "foo.pkg.tar.zst", strings.Repeat("a", maxPackageNameBytes+1)}
	for _, manager := range []string{"apt-get", "dnf", "yum", "pacman", "apk"} {
		t.Run(manager, func(t *testing.T) {
			for _, name := range bad {
				if validPackage(name, manager) {
					t.Errorf("accepted dangerous token %q", name)
				}
				for _, action := range []string{"install", "remove", "uninstall"} {
					if got, err := planCommands(systemInfo{Manager: manager}, []string{"curl", name}, action, ""); err == nil || got != nil {
						t.Errorf("planned dangerous %s token %q: %v, %v", action, name, got, err)
					}
				}
			}
		})
	}
	for _, name := range []string{"curl-", "curl+", "curl-:amd64", "curl+:amd64", "Curl", "foo_bar", "x", "libc6:amd64:foo"} {
		if validPackage(name, "apt-get") {
			t.Errorf("accepted unsafe/invalid APT name %q", name)
		}
	}
}

func TestModelPackageBounds(t *testing.T) {
	packages := make([]string, maxModelPackages)
	for i := range packages {
		packages[i] = fmt.Sprintf("package-%d", i)
	}
	raw := "[apt-get]\n" + strings.Join(packages, "\n")
	if got, err := parsePackages([]byte(raw), "apt-get"); err != nil || len(got) != 256 {
		t.Fatalf("256 entries rejected: %d, %v", len(got), err)
	}
	if _, err := parsePackages([]byte(raw+"\nextra"), "apt-get"); err == nil {
		t.Fatal("accepted 257 entries")
	}
	if _, err := parsePackages([]byte(raw+"\n[apk]\n"+strings.Join(packages, "\n")), "apk"); err != nil {
		t.Fatalf("bounds must be per manager: %v", err)
	}
	for _, selection := range [][]string{nil, {}, append(slices.Clone(packages), "extra")} {
		if _, err := planCommands(systemInfo{Manager: "apt-get"}, selection, "install", ""); err == nil {
			t.Fatal("accepted empty/oversized selection")
		}
	}
	if _, err := planCommands(systemInfo{Manager: "apt-get"}, packages, "install", ""); err != nil {
		t.Fatal(err)
	}
	if !validPackage(strings.Repeat("a", 128), "apt-get") {
		t.Fatal("128-byte package token rejected")
	}
}

func TestModelPlansExactArgv(t *testing.T) {
	aptOptions := []string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold", "-o", "APT::Get::AutomaticRemove=false", "-o", "APT::Get::Purge=false"}
	for _, tc := range []struct {
		manager                  string
		refresh, install, remove []string
	}{
		{"apt-get", []string{"apt-get", "update", "-o", "APT::Update::Error-Mode=any"}, append([]string{"apt-get", "install", "-y", "--no-remove"}, aptOptions...), append([]string{"apt-get", "remove", "-y"}, aptOptions...)},
		{"dnf", []string{"dnf", "makecache", "-y"}, []string{"dnf", "install", "-y"}, []string{"dnf", "remove", "-y", "--setopt=clean_requirements_on_remove=False"}},
		{"yum", []string{"yum", "makecache", "-y"}, []string{"yum", "install", "-y"}, []string{"yum", "remove", "-y", "--setopt=clean_requirements_on_remove=False"}},
		{"pacman", nil, []string{"pacman", "-S", "--needed", "--noconfirm"}, []string{"pacman", "-R", "--noconfirm"}},
		{"apk", []string{"apk", "update"}, []string{"apk", "add"}, []string{"apk", "del"}},
	} {
		for _, action := range []string{"install", "remove", "uninstall"} {
			t.Run(tc.manager+"/"+action, func(t *testing.T) {
				selection := []string{"curl", "git", "curl"}
				before := slices.Clone(selection)
				got, err := planCommands(systemInfo{Manager: tc.manager}, selection, action, "")
				if err != nil {
					t.Fatal(err)
				}
				args, label := tc.remove, "Remove selected packages"
				var want []command
				if action == "install" {
					args, label = tc.install, "Install selected packages"
					if tc.refresh != nil {
						want = append(want, command{Args: tc.refresh, Label: "Refresh package cache"})
					}
				}
				want = append(want, command{Args: append(slices.Clone(args), "--", "curl", "git"), Label: label})
				if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(selection, before) {
					t.Fatalf("got %#v; want %#v; input %v", got, want, selection)
				}
				selection[0] = "changed"
				if slices.Contains(got[len(got)-1].Args, "changed") {
					t.Fatal("plan aliases caller's selection")
				}
			})
		}
	}
	for _, action := range []string{"update", "upgrade", "purge", "autoremove", "", "INSTALL", "use", "status"} {
		if _, err := planCommands(systemInfo{Manager: "apt-get"}, []string{"curl"}, action, ""); err == nil {
			t.Fatalf("accepted operation %q", action)
		}
	}
	if _, err := planCommands(systemInfo{Manager: "sh"}, []string{"curl"}, "install", ""); err == nil {
		t.Fatal("accepted arbitrary executable")
	}
}

func TestModelTemporarySourcesPlan(t *testing.T) {
	s := systemInfo{ID: "ubuntu", Manager: "apt-get", Ubuntu: true, Codename: "noble"}
	file := "/tmp/atm source/repositories.list"
	got, err := planCommands(s, []string{"curl"}, "install", file)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	options := []string{"-o", "Dir::Etc::sourcelist=" + file, "-o", "Dir::Etc::sourceparts=/dev/null", "-o", "APT::Get::List-Cleanup=0"}
	for _, c := range got {
		if !reflect.DeepEqual(c.Args[1:7], options) {
			t.Fatalf("source options not identical on update and install: %v", c.Args)
		}
	}
	got[0].Args[2] = "changed"
	if got[1].Args[2] == "changed" {
		t.Fatal("commands share mutable source argv")
	}
	for _, file := range []string{"relative.list", "/", "/tmp/../etc/sources.list", "/tmp//file", "/tmp/file/", "/tmp/$(id)", "/tmp/a\nfile", "/tmp/a\x00file", "/tmp/a;id", "/tmp/`id`", "/tmp/a*", "/tmp/a\\file", "/tmp/\xff", "/" + strings.Repeat("a", maxModelLineBytes)} {
		if _, err := planCommands(s, []string{"curl"}, "install", file); err == nil {
			t.Fatalf("accepted unsafe source path %q", file)
		}
	}
	for _, changed := range []systemInfo{
		{ID: "debian", Manager: "apt-get", Ubuntu: true, Codename: "noble"},
		{ID: "mint", Manager: "apt-get", Ubuntu: true, Codename: "noble"},
		{ID: "ubuntu", Manager: "dnf", Ubuntu: true, Codename: "noble"},
		{ID: "ubuntu", Manager: "apt-get", Ubuntu: false, Codename: "noble"},
		{ID: "ubuntu", Manager: "apt-get", Ubuntu: true},
		{ID: "ubuntu", Manager: "apt-get", Ubuntu: true, Codename: "resolute"},
	} {
		if _, err := planCommands(changed, []string{"curl"}, "install", file); err == nil {
			t.Fatalf("accepted source override for %#v", changed)
		}
	}
	for _, action := range []string{"remove", "uninstall"} {
		if _, err := planCommands(s, []string{"curl"}, action, file); err == nil {
			t.Fatal("source override permitted on removal")
		}
	}
}

func TestModelUbuntuSourcesReviewedMapping(t *testing.T) {
	for _, id := range []string{"ubuntu", "zorin"} {
		for _, suite := range []string{"jammy", "noble"} {
			for _, arch := range []string{"amd64", "arm64"} {
				s := systemInfo{ID: id, Manager: "apt-get", Ubuntu: true, Codename: suite}
				got, err := ubuntuSources(s, arch)
				archive, security := "https://archive.ubuntu.com/ubuntu", "https://security.ubuntu.com/ubuntu"
				if arch == "arm64" {
					archive, security = "https://ports.ubuntu.com/ubuntu-ports", "https://ports.ubuntu.com/ubuntu-ports"
				}
				var want strings.Builder
				for _, suffix := range []string{"", "-updates", "-backports", "-security"} {
					mirror := archive
					if suffix == "-security" {
						mirror = security
					}
					fmt.Fprintf(&want, "deb [arch=%s signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] %s %s%s main restricted universe multiverse\n", arch, mirror, suite, suffix)
				}
				if err != nil || got != want.String() {
					t.Fatalf("%s/%s/%s: got %q, %v; want %q", id, suite, arch, got, err, want.String())
				}
			}
		}
	}
	for _, suite := range []string{"", "resolute", "focal", "unknown", "stable", "noble-security", "noble\ndeb evil", "$(id)", "../../noble", "noble;id"} {
		s := systemInfo{ID: "ubuntu", Manager: "apt-get", Ubuntu: true, Codename: suite}
		if got, err := ubuntuSources(s, "amd64"); err == nil || got != "" {
			t.Fatalf("accepted unreviewed/invalid suite %q", suite)
		}
		if _, err := planCommands(s, []string{"curl"}, "install", "/tmp/sources.list"); err == nil {
			t.Fatalf("source plan bypassed suite validation for %q", suite)
		}
		if _, err := planCommands(s, []string{"curl"}, "install", ""); err != nil {
			t.Fatalf("configured repositories unexpectedly require source suite: %v", err)
		}
	}
	for _, s := range []systemInfo{{ID: "debian", Manager: "apt-get", Ubuntu: true, Codename: "noble"}, {ID: "ubuntu", Manager: "dnf", Ubuntu: true, Codename: "noble"}, {ID: "ubuntu", Manager: "apt-get", Codename: "noble"}} {
		if _, err := ubuntuSources(s, "amd64"); err == nil {
			t.Fatal("inconsistent/non-Ubuntu system accepted")
		}
	}
	for _, arch := range []string{"", "386", "x86_64", "aarch64", "ARM64", "arm64\ndeb evil"} {
		if _, err := ubuntuSources(systemInfo{ID: "ubuntu", Manager: "apt-get", Ubuntu: true, Codename: "noble"}, arch); err == nil {
			t.Fatalf("accepted unsupported architecture %q", arch)
		}
	}
}

func FuzzModelData(f *testing.F) {
	for _, seed := range []string{"ID=ubuntu\nUBUNTU_CODENAME=noble", "[apt-get]\ncurl", "[apk]\npy3-pip", "ID=$(id)", "[apt-get]\n--help", ""} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = detectSystem(raw, false)
		_, _ = detectSystem(raw, true)
		for _, manager := range []string{"apt-get", "dnf", "yum", "pacman", "apk"} {
			packages, err := parsePackages(raw, manager)
			if err != nil {
				continue
			}
			if len(packages) == 0 || len(packages) > 256 {
				t.Fatal("parser returned unbounded selection")
			}
			for _, action := range []string{"install", "remove"} {
				if _, err := planCommands(systemInfo{Manager: manager}, packages, action, ""); err != nil {
					t.Fatalf("parser/planner contract mismatch: %v", err)
				}
			}
		}
	})
}
