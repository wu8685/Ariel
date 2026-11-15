package appserver

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	if got := shellQuote(`/tmp/My Agent's/codex`); got != `'/tmp/My Agent'\''s/codex'` {
		t.Fatalf("quote=%s", got)
	}
}

func TestGoChildStartedWithPipesStaysAlive(t *testing.T) {
	if os.Getenv("ARIEL_HARNESS_CHILD") == "1" {
		time.Sleep(30 * time.Second)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestGoChildStartedWithPipesStaysAlive$")
	cmd.Env = append(os.Environ(), "ARIEL_HARNESS_CHILD=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	defer stdout.Close()
	if err := startCommand(cmd); err != nil {
		t.Fatal(err)
	}
	defer func() {
		killCommand(cmd)
		_ = cmd.Wait()
	}()
	time.Sleep(400 * time.Millisecond)
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatalf("piped child died: %v", err)
	}
}
