//go:build !windows

package appserver

import (
	"os"
	"syscall"
)

type processExitStatus syscall.WaitStatus

func processWaitStatus(state *os.ProcessState) (processExitStatus, bool) {
	status, ok := state.Sys().(syscall.WaitStatus)
	return processExitStatus(status), ok
}

func (status processExitStatus) signaled() bool {
	return syscall.WaitStatus(status).Signaled()
}

func (status processExitStatus) signal() string {
	return syscall.WaitStatus(status).Signal().String()
}
