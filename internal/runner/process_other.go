//go:build !linux && !darwin && !windows

package runner

import (
	"fmt"
	"os/exec"
)

type processTree struct{}

func prepareProcess(*exec.Cmd) (*processTree, error) {
	return nil, fmt.Errorf("native execution is supported on Linux, macOS and Windows only")
}
func (*processTree) attach(*exec.Cmd) error { return nil }
func (*processTree) kill()                  {}
func (*processTree) close()                 {}
