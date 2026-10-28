package main

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/AtomyCloud/atm/internal/data"
)

const (
	maxOSReleaseBytes   = 64 << 10
	maxPackageFileBytes = 1 << 20
	maxModelLineBytes   = 4096
	maxModelPackages    = 256
	maxPackageNameBytes = 128
)

type systemInfo struct {
	ID, Name, Manager, Codename string
	Ubuntu                      bool
}

type command struct {
	Args  []string
	Label string
}

var (
	osAssignment = regexp.MustCompile(`^[A-Z][A-Z0-9_]*=(?:[A-Za-z0-9_.:/+-]*|'[^']*'|"(?:[^"\\]|\\.)*")$`)
	osIdentifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	aptPackage   = regexp.MustCompile(`^[a-z0-9][a-z0-9+-]+(:[a-z0-9][a-z0-9-]*)?$`)
	rpmPackage   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+_.-]*$`)
	archPackage  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@+_.-]*$`)
	apkPackage   = regexp.MustCompile(`^[a-z0-9][a-z0-9+_.-]*$`)
)

// modelLines bounds parsing before allocation and accepts CRLF only at line ends.
func modelLines(raw []byte, limit int) ([]string, error) {
	if len(raw) > limit || !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid or oversized input")
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if len(line) > maxModelLineBytes {
			return nil, fmt.Errorf("line %d exceeds size limit", i+1)
		}
		for _, r := range line {
			if (unicode.IsControl(r) && r != '\t') || (!unicode.IsPrint(r) && r != '\t') {
				return nil, fmt.Errorf("line %d contains a control character", i+1)
			}
		}
		lines[i] = strings.Trim(line, " \t")
	}
	return lines, nil
}

func detectSystem(raw []byte, archMarker bool) (systemInfo, error) {
	lines, err := modelLines(raw, maxOSReleaseBytes)
	if err != nil {
		return systemInfo{}, err
	}
	seen := make(map[string]bool)
	var assignments strings.Builder
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !osAssignment.MatchString(line) {
			return systemInfo{}, fmt.Errorf("os-release line %d is not a literal assignment", i+1)
		}
		key, _, _ := strings.Cut(line, "=")
		if seen[key] {
			return systemInfo{}, fmt.Errorf("duplicate os-release key %s", key)
		}
		seen[key] = true
		assignments.WriteString(line)
		assignments.WriteByte('\n')
	}
	// ParseAssignments walks a syntax tree and rejects expansions; it never executes.
	values, err := data.ParseAssignments([]byte(assignments.String()), "os-release")
	if err != nil {
		return systemInfo{}, err
	}
	s := systemInfo{ID: values["ID"], Name: values["NAME"]}
	if s.ID != "" && !osIdentifier.MatchString(s.ID) {
		return systemInfo{}, fmt.Errorf("invalid OS ID")
	}
	likes := strings.Fields(values["ID_LIKE"])
	for _, like := range likes {
		if !osIdentifier.MatchString(like) {
			return systemInfo{}, fmt.Errorf("invalid ID_LIKE token")
		}
	}
	for _, r := range s.Name {
		if !unicode.IsPrint(r) {
			return systemInfo{}, fmt.Errorf("invalid OS name")
		}
	}
	legacyName := s.ID
	switch s.ID {
	case "ubuntu", "zorin", "debian":
		s.Manager = "apt-get"
		s.Ubuntu = s.ID == "ubuntu" || s.ID == "zorin"
	case "centos":
		s.Manager = "yum"
	case "rocky", "rockylinux":
		s.Manager, legacyName = "dnf", "rockylinux"
	case "almalinux", "rhel", "fedora":
		s.Manager = "dnf"
	case "arch", "archlinux":
		s.Manager, legacyName = "pacman", "archlinux"
	case "alpine":
		s.Manager = "apk"
	default:
		if archMarker {
			s.Manager, legacyName = "pacman", "archlinux"
		} else {
			// ID_LIKE is ordered by closest relationship, not substring matching.
			for _, like := range likes {
				switch like {
				case "debian", "ubuntu":
					s.Manager = "apt-get"
					if legacyName == "" {
						legacyName = "debian-like"
					}
				case "rhel", "fedora":
					s.Manager = "dnf"
					if legacyName == "" {
						legacyName = "rhel-like"
					}
				}
				if s.Manager != "" {
					break
				}
			}
		}
	}
	if s.Manager == "" {
		return systemInfo{}, fmt.Errorf("unsupported operating system %q", s.ID)
	}
	if s.Name == "" {
		s.Name = legacyName
	}
	// Codenames are inert metadata here; only optional source mode restricts suites.
	s.Codename = values["VERSION_CODENAME"]
	if value, exists := values["UBUNTU_CODENAME"]; exists {
		s.Codename = value
	}
	for _, r := range s.Codename {
		if !unicode.IsPrint(r) {
			return systemInfo{}, fmt.Errorf("invalid release codename metadata")
		}
	}
	return s, nil
}

func packagePattern(manager string) *regexp.Regexp {
	switch manager {
	case "apt-get":
		return aptPackage
	case "dnf", "yum":
		return rpmPackage
	case "pacman":
		return archPackage
	case "apk":
		return apkPackage
	}
	return nil
}

func validPackage(name, manager string) bool {
	pattern := packagePattern(manager)
	if pattern == nil || len(name) > maxPackageNameBytes || !pattern.MatchString(name) {
		return false
	}
	base, _, _ := strings.Cut(name, ":")
	// Dotted APT names are excluded above: missing names can fall back to regexes.
	// APT interprets trailing +/- as action overrides even in literal argv.
	if manager == "apt-get" && (strings.HasSuffix(base, "+") || strings.HasSuffix(base, "-")) {
		return false
	}
	for _, suffix := range []string{".deb", ".rpm", ".apk", ".pkg.tar", ".pkg.tar.gz", ".pkg.tar.xz", ".pkg.tar.zst"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}

func parsePackages(raw []byte, manager string) ([]string, error) {
	if packagePattern(manager) == nil {
		return nil, fmt.Errorf("unsupported package manager %q", manager)
	}
	lines, err := modelLines(raw, maxPackageFileBytes)
	if err != nil {
		return nil, err
	}
	sections, seen := make(map[string]bool), make(map[string]bool)
	var packages []string
	section, count := "", 0
	for i, line := range lines {
		// Only full comments or whitespace-delimited trailing comments are allowed.
		if index := strings.IndexByte(line, '#'); index >= 0 && (index == 0 || line[index-1] == ' ' || line[index-1] == '\t') {
			line = strings.Trim(line[:index], " \t")
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("invalid package section on line %d", i+1)
			}
			section = line[1 : len(line)-1]
			if packagePattern(section) == nil || sections[section] {
				return nil, fmt.Errorf("unknown or duplicate package section on line %d", i+1)
			}
			sections[section] = true
			count = 0
			continue
		}
		count++
		if section == "" || count > maxModelPackages || !validPackage(line, section) {
			return nil, fmt.Errorf("invalid package entry on line %d (one package per line required)", i+1)
		}
		if section == manager && !seen[line] {
			seen[line] = true
			packages = append(packages, line)
		}
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("no packages in [%s]", manager)
	}
	return packages, nil
}

func ubuntuSystem(s systemInfo) bool {
	return s.Ubuntu && s.Manager == "apt-get" && (s.ID == "ubuntu" || s.ID == "zorin")
}

// Only reviewed LTS suites are eligible for the optional official-source mode.
// In particular, newer arm64 suites must not be assumed to use the ports archive.
func reviewedUbuntuSuite(s systemInfo) bool {
	return ubuntuSystem(s) && (s.Codename == "jammy" || s.Codename == "noble")
}

func planCommands(s systemInfo, packages []string, action, sourcePath string) ([]command, error) {
	if packagePattern(s.Manager) == nil {
		return nil, fmt.Errorf("unsupported package manager %q", s.Manager)
	}
	if action != "install" && action != "remove" && action != "uninstall" {
		return nil, fmt.Errorf("unsupported package action %q", action)
	}
	if len(packages) == 0 || len(packages) > maxModelPackages {
		return nil, fmt.Errorf("package selection is empty or oversized")
	}
	selected, seen := make([]string, 0, len(packages)), make(map[string]bool)
	for _, name := range packages {
		if !validPackage(name, s.Manager) {
			return nil, fmt.Errorf("invalid package selection %q", name)
		}
		if !seen[name] {
			seen[name] = true
			selected = append(selected, name)
		}
	}
	var sourceOptions []string
	if sourcePath != "" {
		if action != "install" || !reviewedUbuntuSuite(s) {
			return nil, fmt.Errorf("temporary sources require Ubuntu/Zorin installation with a reviewed jammy or noble base")
		}
		if !utf8.ValidString(sourcePath) || !path.IsAbs(sourcePath) || path.Clean(sourcePath) != sourcePath || sourcePath == "/" || len(sourcePath) > maxModelLineBytes {
			return nil, fmt.Errorf("source list must be a clean absolute file path")
		}
		for _, r := range sourcePath {
			if !unicode.IsPrint(r) || strings.ContainsRune("\\\"'`$;|&<>()[]{}*?!", r) {
				return nil, fmt.Errorf("invalid source list path")
			}
		}
		sourceOptions = []string{"-o", "Dir::Etc::sourcelist=" + sourcePath, "-o", "Dir::Etc::sourceparts=/dev/null", "-o", "APT::Get::List-Cleanup=0"}
	}
	var commands []command
	var args []string
	if action == "install" {
		switch s.Manager {
		case "apt-get":
			refresh := append([]string{"apt-get"}, sourceOptions...)
			commands = append(commands, command{Args: append(refresh, "update", "-o", "APT::Update::Error-Mode=any"), Label: "Refresh package cache"})
			args = append([]string{"apt-get"}, sourceOptions...)
			args = append(args, "install", "-y", "--no-remove")
		case "dnf", "yum":
			commands = append(commands, command{Args: []string{s.Manager, "makecache", "-y"}, Label: "Refresh package cache"})
			args = []string{s.Manager, "install", "-y"}
		case "pacman":
			// Parent must explain that database/system upgrades are separate manual work.
			args = []string{"pacman", "-S", "--needed", "--noconfirm"}
		case "apk":
			commands = append(commands, command{Args: []string{"apk", "update"}, Label: "Refresh package cache"})
			args = []string{"apk", "add"}
		}
	} else {
		switch s.Manager {
		case "apt-get":
			args = []string{"apt-get", "remove", "-y"}
		case "dnf", "yum":
			args = []string{s.Manager, "remove", "-y", "--setopt=clean_requirements_on_remove=False"}
		case "pacman":
			args = []string{"pacman", "-R", "--noconfirm"}
		case "apk":
			args = []string{"apk", "del"}
		}
	}
	if s.Manager == "apt-get" {
		// Parent supplies DEBIAN_FRONTEND=noninteractive in the trusted exec environment.
		args = append(args, "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold",
			"-o", "APT::Get::AutomaticRemove=false", "-o", "APT::Get::Purge=false")
	}
	label := "Remove selected packages"
	if action == "install" {
		label = "Install selected packages"
	}
	args = append(args, "--")
	args = append(args, selected...)
	return append(commands, command{Args: args, Label: label}), nil
}

func ubuntuSources(s systemInfo, arch string) (string, error) {
	if !reviewedUbuntuSuite(s) {
		return "", fmt.Errorf("official Ubuntu sources require Ubuntu/Zorin with a reviewed jammy or noble base")
	}
	archive, security := "", ""
	switch arch {
	case "amd64":
		archive, security = "https://archive.ubuntu.com/ubuntu", "https://security.ubuntu.com/ubuntu"
	case "arm64":
		archive, security = "https://ports.ubuntu.com/ubuntu-ports", "https://ports.ubuntu.com/ubuntu-ports"
	default:
		return "", fmt.Errorf("unsupported Ubuntu architecture %q", arch)
	}
	var sources strings.Builder
	for _, suffix := range []string{"", "-updates", "-backports", "-security"} {
		mirror := archive
		if suffix == "-security" {
			mirror = security
		}
		fmt.Fprintf(&sources, "deb [arch=%s signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] %s %s%s main restricted universe multiverse\n", arch, mirror, s.Codename, suffix)
	}
	return sources.String(), nil
}
