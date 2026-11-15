//go:build windows

package agy

import (
	"errors"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	processStillActive             = 259
)

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// ERROR_ACCESS_DENIED indicates a live elevated/protected process; any other error means no active PID.
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = syscall.CloseHandle(h) }()

	var exitCode uint32
	if err := syscall.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == processStillActive
}
