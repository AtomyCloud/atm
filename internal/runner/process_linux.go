package runner

import (
	"os/exec"
	"syscall"
)

type processTree struct{ pid int }

func prepareProcess(cmd *exec.Cmd) (*processTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processTree{}, nil
}
func (p *processTree) attach(cmd *exec.Cmd) error { p.pid = cmd.Process.Pid; return nil }
func (p *processTree) kill() {
	if p.pid != 0 {
		_ = syscall.Kill(-p.pid, syscall.SIGKILL)
	}
}
func (p *processTree) close() { p.pid = 0 }
