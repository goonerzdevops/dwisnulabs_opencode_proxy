//go:build windows

package proxy

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureChildCmd(_ *exec.Cmd) {}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

func initKillOnCloseJob() {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(h)
		return
	}
	jobHandle = h
}

// bindChildToParentJob attaches proc to a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE so no zombie wireproxy.exe survives if
// opencode-proxy.exe exits or crashes.
func bindChildToParentJob(proc *os.Process) {
	if proc == nil || proc.Pid <= 0 {
		return
	}
	jobOnce.Do(initKillOnCloseJob)
	if jobHandle == 0 {
		return
	}
	pHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(proc.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(pHandle)
	_ = windows.AssignProcessToJobObject(jobHandle, pHandle)
}
