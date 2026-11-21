//go:build linux

package proxy

import (
	"os"
	"os/exec"
	"syscall"
)

// configureChildCmd ensures Linux kernel sends SIGTERM to child wireproxy
// processes immediately if opencode-proxy ever exits unexpectedly.
func configureChildCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
}

func bindChildToParentJob(_ *os.Process) {}
