//go:build !windows && !linux

package proxy

import (
	"os"
	"os/exec"
)

func configureChildCmd(_ *exec.Cmd) {}

func bindChildToParentJob(_ *os.Process) {}
