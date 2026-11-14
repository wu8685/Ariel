package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type harnessProc struct {
	pid, ppid    int
	cwd, command string
}

func killHarnessProcesses(self int, root string) {
	procs := listHarnessProcesses()
	descendants := map[int]bool{}
	collectDescendants(procs, self, descendants)
	var victims []int
	for _, proc := range procs {
		if proc.pid <= 1 || proc.pid == self || !descendants[proc.pid] {
			continue
		}
		if proc.cwd == "" {
			proc.cwd = processCWD(proc.pid)
		}
		if strings.Contains(proc.command, "app-server") || strings.Contains(proc.command, "llama-server") || pathUnder(proc.cwd, root) {
			victims = append(victims, proc.pid)
		}
	}
	signalPIDs(victims, syscall.SIGTERM)
	left := waitGone(victims, time.Second)
	signalPIDs(left, syscall.SIGKILL)
	_ = waitGone(left, time.Second)
}

func collectDescendants(procs []harnessProc, parent int, into map[int]bool) {
	for _, proc := range procs {
		if proc.ppid != parent || into[proc.pid] {
			continue
		}
		into[proc.pid] = true
		collectDescendants(procs, proc.pid, into)
	}
}

func signalPIDs(pids []int, signal syscall.Signal) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, signal)
	}
}

func waitGone(pids []int, budget time.Duration) []int {
	deadline := time.Now().Add(budget)
	left := append([]int(nil), pids...)
	for len(left) > 0 && time.Now().Before(deadline) {
		next := left[:0]
		for _, pid := range left {
			if syscall.Kill(pid, 0) == nil {
				next = append(next, pid)
			}
		}
		left = append([]int(nil), next...)
		if len(left) > 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return left
}

func pathUnder(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func listHarnessProcesses() []harnessProc {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return listProcessesPS()
	}
	var procs []harnessProc
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		procs = append(procs, readProc(pid))
	}
	return procs
}

func readProc(pid int) harnessProc {
	proc := harnessProc{pid: pid, cwd: processCWD(pid)}
	if body, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status"); err == nil {
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "PPid:") {
				proc.ppid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
			}
		}
	}
	if body, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline"); err == nil {
		proc.command = string(bytes.ReplaceAll(body, []byte{0}, []byte{' '}))
	}
	return proc
}

func processCWD(pid int) string {
	if target, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil {
		return target
	}
	return ""
}

func listProcessesPS() []harnessProc {
	body, err := exec.Command("ps", "-ax", "-o", "pid=", "-o", "ppid=", "-o", "command=").Output()
	if err != nil {
		return nil
	}
	var procs []harnessProc
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		procs = append(procs, harnessProc{pid: pid, ppid: ppid, command: strings.Join(fields[2:], " ")})
	}
	return procs
}
