package tui

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
)

type terminalCommand struct {
	cmd    *exec.Cmd
	notice string
	in     io.Reader
	out    io.Writer
}

func (c *terminalCommand) SetStdin(r io.Reader)  { c.in = r; c.cmd.Stdin = r }
func (c *terminalCommand) SetStdout(w io.Writer) { c.out = w; c.cmd.Stdout = w }
func (c *terminalCommand) SetStderr(w io.Writer) { c.cmd.Stderr = w }
func (c *terminalCommand) Run() error {
	err := c.cmd.Run()
	if err != nil {
		fmt.Fprintln(c.out, err)
	}
	fmt.Fprintln(c.out, "\n"+c.notice)
	_, _ = bufio.NewReader(c.in).ReadString('\n')
	return err
}
