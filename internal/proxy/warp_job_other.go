//go:build !windows

package proxy

import "os"

func bindChildToParentJob(_ *os.Process) {}
