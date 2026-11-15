//go:build windows

package appserver

import "os"

type processExitStatus struct{}

func processWaitStatus(*os.ProcessState) (processExitStatus, bool) {
	return processExitStatus{}, false
}

func (processExitStatus) signaled() bool { return false }
func (processExitStatus) signal() string { return "" }
