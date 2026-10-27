// Package runner executes trusted native plugins and the Linux compatibility bridge.
package runner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/AtomyCloud/atm/internal/config"
	"github.com/AtomyCloud/atm/internal/plugin"
)

type Plan struct {
	Schema    int      `json:"schema"`
	Executor  string   `json:"executor"`
	Protocol  string   `json:"protocol"`
	Command   []string `json:"command"`
	Directory string   `json:"directory"`
	DryRun    bool     `json:"dry_run"`
	PluginID  string   `json:"plugin_id,omitempty"`
	Operation string   `json:"operation,omitempty"`
	Arguments []string `json:"arguments,omitempty"`
}

func Plugin(c config.Config, p plugin.Plugin, action string, args []string) (Plan, error) {
	if !p.Available {
		return Plan{}, fmt.Errorf("%s: %s", p.ID, p.Reason)
	}
	if !plugin.IDPattern.MatchString(p.ID) || !slices.Contains(p.Operations, action) {
		return Plan{}, fmt.Errorf("unsupported operation: %s %s", p.ID, action)
	}
	if (action == "remove" || action == "use") && len(args) != 1 {
		return Plan{}, fmt.Errorf("%s requires one version", action)
	}
	if (action == "menu" || action == "status" || action == "uninstall") && len(args) != 0 {
		return Plan{}, fmt.Errorf("%s takes no extra arguments", action)
	}
	if p.Executor == "native" {
		if len(args) > 128 {
			return Plan{}, fmt.Errorf("too many native arguments")
		}
		if err := p.Manifest.Validate(p.ID); err != nil {
			return Plan{}, err
		}
		exe, err := p.NativeExecutable(runtime.GOOS + "/" + runtime.GOARCH)
		if err != nil {
			return Plan{}, err
		}
		return Plan{Schema: 2, Executor: "native", Protocol: "jsonl-v1", Command: []string{exe}, Directory: p.Directory, DryRun: c.DryRun, PluginID: p.ID, Operation: action, Arguments: args}, nil
	}
	if runtime.GOOS != "linux" {
		return Plan{}, fmt.Errorf("Bash compatibility requires Linux")
	}
	command := []string{"bash", filepath.Join(c.Home, "libexec", "atm-legacy.sh"), action, p.ID}
	return Plan{Schema: 1, Executor: "legacy-bash", Protocol: "tty-v1", Command: append(command, args...), Directory: c.Home, DryRun: c.DryRun}, nil
}

func Core(c config.Config, args []string) (Plan, error) {
	if runtime.GOOS != "linux" {
		return Plan{}, fmt.Errorf("this operation requires the Linux compatibility backend")
	}
	if len(args) == 0 {
		return Plan{}, fmt.Errorf("missing legacy operation")
	}
	allowed := false
	switch args[0] {
	case "path":
		allowed = len(args) == 2 && args[1] == "apply"
	case "install":
		allowed = len(args) == 1
	case "plugins":
		if len(args) > 1 {
			switch args[1] {
			case "search":
				allowed = true
			case "download":
				allowed = len(args) == 3 && plugin.IDPattern.MatchString(args[2])
			case "check-updates":
				allowed = len(args) == 2
			case "update":
				allowed = len(args) == 3 && (args[2] == "--all" || plugin.IDPattern.MatchString(args[2]))
			}
		}
	case "updates":
		if len(args) == 2 {
			allowed = slices.Contains([]string{"check", "all", "changes", "backups"}, args[1])
		}
		if len(args) == 3 {
			allowed = args[1] == "plugin" && plugin.IDPattern.MatchString(args[2])
		}
	case "self-update":
		allowed = len(args) == 2 && args[1] == "--check"
	}
	if !allowed {
		return Plan{}, fmt.Errorf("operation not enabled in the migration: %s; use the original bin/atm explicitly for legacy administration", strings.Join(args, " "))
	}
	command := []string{"bash", filepath.Join(c.Home, "bin", "atm")}
	return Plan{Schema: 1, Executor: "legacy-bash", Protocol: "tty-v1", Command: append(command, args...), Directory: c.Home, DryRun: c.DryRun}, nil
}

func Command(c config.Config, plan Plan) (*exec.Cmd, error) {
	if c.DryRun || plan.DryRun {
		return nil, fmt.Errorf("dry-run plans must not be executed")
	}
	if len(plan.Command) < 2 || plan.Command[0] != "bash" {
		return nil, fmt.Errorf("invalid execution plan")
	}
	if _, err := os.Lstat(filepath.Join(c.Home, ".atm-core-update-pending")); !os.IsNotExist(err) {
		return nil, fmt.Errorf("legacy CORE recovery pending; execution blocked")
	}
	if _, err := os.Stat(plan.Command[1]); err != nil {
		return nil, err
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		return nil, fmt.Errorf("Linux compatibility needs Bash: %w", err)
	}
	cmd := exec.Command(bash, plan.Command[1:]...)
	cmd.Dir = plan.Directory
	values := map[string]string{"ATM_HOME": c.Home, "ATM_PLUGIN_DIR": c.PluginsDir, "ATM_CONFIG_DIR": c.ConfigDir,
		"ATM_APPS_DIR": c.AppsDir, "ATM_LANG": c.Language, "ATM_PLUGIN_SORT_ORDER": c.Sort,
		"ATM_DRY_RUN": "0", "BASH_ENV": "", "ENV": ""}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if _, override := values[key]; !override {
			cmd.Env = append(cmd.Env, value)
		}
	}
	for key, value := range values {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd, nil
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var native *NativeError
	if errors.As(err, &native) {
		return native.Outcome.ExitCode
	}
	if e, ok := err.(*exec.ExitError); ok {
		if code := e.ExitCode(); code > 0 {
			return code
		}
		return 130
	}
	return 1
}
