package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The fixture compiler only records orchestration and creates inert outputs.
const buildManagerCompiler = `#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "$BUILD_TEST_LOG"
case "$1" in
  list) printf '1.26.0\n' ;;
  version) printf 'fixture compiler\n' ;;
  build)
    while [[ "$1" != -o ]]; do shift; done
    printf 'inert CORE binary\n' > "$2"
    ;;
  run)
    shift 2
    node=0; out=; os=; arch=
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -node-only) node=1; shift ;;
        -plugins-only) node=1; shift ;;
        -out) out="$2"; shift 2 ;;
        -os) os="$2"; shift 2 ;;
        -arch) arch="$2"; shift 2 ;;
        *) exit 2 ;;
      esac
    done
    if [[ "$node" == 1 ]]; then
      [[ "${BUILD_TEST_FAIL_NODE:-0}" != 1 ]] || exit 7
      [[ "$os" == linux ]] || exit 0
      mkdir -p "$out/plugins/node/bin/$os-$arch"
      printf 'inert Node binary\n' > "$out/plugins/node/bin/$os-$arch/atm-node"
      printf 'inert checksum\n' > "$out/plugins/node/bin/$os-$arch/atm-node.sha256"
    fi
    ;;
  *) exit 2 ;;
esac
`

func TestBuildManagerNativeNode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash build orchestration is tested on Unix")
	}
	script, err := os.ReadFile("../../scripts/build_manager.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"binaries", "packages", "local", "plugins-local", "unsupported-local", "failed-node", "local-out-refused", "plugins-out-refused", "conflicting-local"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			write := func(name, contents string) {
				t.Helper()
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
					t.Fatal(err)
				}
			}
			write("scripts/build_manager.sh", string(script))
			write("cmd/atm/main.go", "package main")
			write("go.mod", "module fixture\ngo 1.26.0\n")
			write("compiler/go", buildManagerCompiler)
			args := []string{filepath.Join(root, "scripts/build_manager.sh"), "--target", "all"}
			switch scenario {
			case "packages":
				args = append(args, "--format", "packages")
			case "local", "unsupported-local", "local-out-refused":
				args = append(args, "--node-local")
				if scenario == "unsupported-local" {
					args[2] = "darwin-amd64"
				}
				if scenario == "local-out-refused" {
					args = append(args, "--out", filepath.Join(root, "elsewhere"))
				}
			case "plugins-local", "plugins-out-refused", "conflicting-local":
				args = append(args, "--plugins-local")
				if scenario == "plugins-out-refused" {
					args = append(args, "--out", filepath.Join(root, "elsewhere"))
				}
				if scenario == "conflicting-local" {
					args = append(args, "--node-local")
				}
			}
			cmd := exec.Command("bash", args...)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "ATM_BUILD_GO="+filepath.Join(root, "compiler/go"), "BUILD_TEST_LOG="+filepath.Join(root, "calls"))
			if scenario == "failed-node" {
				cmd.Env = append(cmd.Env, "BUILD_TEST_FAIL_NODE=1")
			}
			output, err := cmd.CombinedOutput()
			if scenario == "unsupported-local" || scenario == "local-out-refused" || scenario == "plugins-out-refused" || scenario == "conflicting-local" {
				if err == nil {
					t.Fatal("invalid local build accepted", string(output))
				}
				if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
					t.Fatal("invalid options invoked compiler")
				}
				return
			}
			if scenario == "failed-node" {
				markers, _ := filepath.Glob(filepath.Join(root, "binary", "*", "BUILD-INCOMPLETE"))
				if err == nil || len(markers) != 1 || strings.Contains(string(output), "Build completed:") {
					t.Fatal("Node failure not propagated", err, string(output))
				}
				return
			}
			if err != nil {
				t.Fatal(err, string(output))
			}
			calls, err := os.ReadFile(filepath.Join(root, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "packages" {
				if strings.Count(string(calls), "run ./tools/package -os ") != 5 {
					t.Fatal("lost CORE package targets", string(calls))
				}
				return
			}
			flag, count := "-plugins-only", 5
			if scenario == "local" {
				flag, count = "-node-only", 2
			}
			if strings.Count(string(calls), "run ./tools/package "+flag) != count {
				t.Fatal("incorrect native build targets", string(calls))
			}
			for _, target := range []string{"linux-amd64", "linux-arm64"} {
				pattern := filepath.Join(root, "plugins", "node", "bin", target, "atm-node")
				if scenario == "binaries" {
					pattern = filepath.Join(root, "binary", "*", target, "plugins", "node", "bin", target, "atm-node.sha256")
				}
				matches, _ := filepath.Glob(pattern)
				if len(matches) != 1 {
					t.Fatal("missing native Node output", pattern)
				}
			}
		})
	}
}
