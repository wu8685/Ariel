package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/probe"
)

func fixtureCommand(ctx context.Context, args []string, out, errout io.Writer) int {
	if len(args) > 0 && (args[0] == "exercise" || args[0] == "stale-interaction" || args[0] == "user-input" || args[0] == "user-input-race" || args[0] == "user-input-interrupt" || args[0] == "command-decline" || args[0] == "command-cancel" || args[0] == "command-pending" || args[0] == "user-input-pending" || args[0] == "file-pending" || args[0] == "permission-pending" || args[0] == "busy-submit") {
		return fixtureControlCommand(ctx, args[1:], out, errout, args[0])
	}
	if len(args) == 0 || args[0] != "create" {
		fmt.Fprintln(errout, "expected fixture create")
		return 2
	}
	cfg, err := probe.Defaults()
	if err != nil {
		return 1
	}
	f := flag.NewFlagSet("fixture create", flag.ContinueOnError)
	f.SetOutput(errout)
	write := f.Bool("write-enabled", false, "explicitly allow an isolated fixture and seed turn")
	manifest := f.String("manifest", "", "new manifest file, outside tracked source")
	parent := f.String("workspace", os.TempDir(), "parent for a newly created temporary fixture workspace")
	purpose := f.String("purpose", "", "optional dedicated fixture purpose: user-input, command-decline, command-cancel, command-accept, file-change, permission-request")
	f.StringVar(&cfg.AppPath, "app", cfg.AppPath, "Desktop app")
	f.StringVar(&cfg.Binary, "codex-binary", "", "matching Desktop binary")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || !*write || *manifest == "" {
		fmt.Fprintln(errout, "requires --write-enabled and --manifest")
		return 2
	}
	if *purpose != "" && *purpose != "user-input" && *purpose != "command-decline" && *purpose != "command-cancel" && *purpose != "command-accept" && *purpose != "file-change" && *purpose != "permission-request" {
		fmt.Fprintln(errout, "unknown fixture purpose")
		return 2
	}
	manifestPath, err := filepath.Abs(*manifest)
	if err != nil {
		return 2
	}
	parentPath, err := filepath.Abs(*parent)
	if err != nil {
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	binary, _, err := probe.SelectBinary(cfg.Binary, probe.BundledBinary(cfg.AppPath), func(p string) (string, error) { return probe.BinaryVersion(ctx, p) })
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	c, err := appserver.Start(ctx, binary, os.TempDir())
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	defer c.Close()
	m, err := probe.CreateFixture(ctx, c, probe.CreateOptions{Parent: parentPath, ManifestPath: manifestPath, WriteEnabled: *write, Purpose: *purpose})
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	err = probe.SeedFixture(ctx, c, m, *write, 500*time.Millisecond)
	result := map[string]any{"fixtureCreated": true, "manifestSaved": true, "seedVerified": err == nil}
	if err != nil {
		result["reason"] = err.Error()
	}
	if json.NewEncoder(out).Encode(result) != nil || err != nil {
		return 1
	}
	return 0
}

func fixtureControlCommand(ctx context.Context, args []string, out, errout io.Writer, operation string) int {
	cfg, err := probe.Defaults()
	if err != nil {
		return 1
	}
	f := flag.NewFlagSet("fixture exercise", flag.ContinueOnError)
	f.SetOutput(errout)
	write := f.Bool("write-enabled", false, "allow fixture-only send and precise stop")
	path := f.String("manifest", "", "manifest from fixture create")
	f.StringVar(&cfg.Socket, "ipc-socket", cfg.Socket, "Desktop IPC socket")
	f.StringVar(&cfg.AppPath, "app", cfg.AppPath, "Desktop app")
	f.StringVar(&cfg.Binary, "codex-binary", "", "matching binary")
	if f.Parse(args) != nil || f.NArg() != 0 || !*write || *path == "" {
		fmt.Fprintln(errout, "requires --write-enabled and --manifest")
		return 2
	}
	m, err := probe.LoadManifest(*path)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	if err := m.Authorize(*write, m.ThreadID, m.Workspace); err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	if (operation == "user-input" || operation == "user-input-race" || operation == "user-input-interrupt") && m.Purpose != "user-input" {
		fmt.Fprintln(errout, "requires a dedicated user-input fixture")
		return 1
	}
	if operation == "command-decline" && m.Purpose != "command-decline" {
		fmt.Fprintln(errout, "requires a dedicated command-decline fixture")
		return 1
	}
	if operation == "command-cancel" && m.Purpose != "command-cancel" {
		fmt.Fprintln(errout, "requires a dedicated command-cancel fixture")
		return 1
	}
	if operation == "command-pending" && m.Purpose != "command-accept" {
		fmt.Fprintln(errout, "requires a dedicated command-accept fixture")
		return 1
	}
	if operation == "user-input-pending" && m.Purpose != "user-input" {
		fmt.Fprintln(errout, "requires a dedicated user-input fixture")
		return 1
	}
	if operation == "file-pending" && m.Purpose != "file-change" {
		fmt.Fprintln(errout, "requires a dedicated file-change fixture")
		return 1
	}
	if operation == "permission-pending" && m.Purpose != "permission-request" {
		fmt.Fprintln(errout, "requires a dedicated permission-request fixture")
		return 1
	}
	if operation == "busy-submit" && m.Purpose != "" {
		fmt.Fprintln(errout, "busy-submit requires a no-tools fixture")
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	binary, version, err := probe.SelectBinary(cfg.Binary, probe.BundledBinary(cfg.AppPath), func(p string) (string, error) { return probe.BinaryVersion(ctx, p) })
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	desktopVersion, _ := probe.DesktopVersion(ctx, cfg.AppPath)
	if !probe.IPCProfileVerified(desktopVersion, version) {
		fmt.Fprintln(errout, "Desktop IPC version profile unverified")
		return 1
	}
	history, err := appserver.Start(ctx, binary, os.TempDir())
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	defer history.Close()
	reader := appserver.HistoryReader{RPC: history}
	thread, err := reader.ReadFull(ctx, m.ThreadID)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	if err := m.Authorize(*write, thread.ID, thread.CWD); err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	c, err := probe.ConnectThread(ctx, cfg.Socket, m.ThreadID)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	defer c.Close()
	if operation == "command-pending" || operation == "user-input-pending" || operation == "file-pending" || operation == "permission-pending" {
		purpose := "command-accept"
		if operation == "user-input-pending" {
			purpose = "user-input"
		}
		if operation == "file-pending" {
			purpose = "file-change"
		}
		if operation == "permission-pending" {
			purpose = "permission-request"
		}
		result, e := c.ProbePendingInteraction(ctx, m.ThreadID, m.Workspace, purpose)
		output := map[string]any{"pendingInteraction": result}
		if e != nil {
			output["reason"] = e.Error()
		}
		if json.NewEncoder(out).Encode(output) != nil || e != nil {
			return 1
		}
		return 0
	}
	if operation == "busy-submit" {
		second, e := probe.ConnectThread(ctx, cfg.Socket, m.ThreadID)
		if e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
		defer second.Close()
		result, e := c.ProbeBusySubmit(ctx, m.ThreadID, m.Workspace, second)
		output := map[string]any{"busySubmit": result}
		if e != nil {
			output["reason"] = e.Error()
		}
		if json.NewEncoder(out).Encode(output) != nil || e != nil {
			return 1
		}
		return 0
	}
	if operation == "user-input-race" {
		second, e := probe.ConnectThread(ctx, cfg.Socket, m.ThreadID)
		if e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
		defer second.Close()
		result, e := c.ProbeUserInputRace(ctx, m.ThreadID, m.Workspace, second)
		verified := false
		if e == nil && result.Completed {
			t, readErr := reader.ReadFull(ctx, m.ThreadID)
			if readErr != nil {
				e = readErr
			} else {
				e = probe.VerifyCompletedReply(t.Turns, result.TurnID, result.Marker)
				verified = e == nil
			}
		}
		output := map[string]any{"userInputRace": result, "persistedExactTurnVerified": verified}
		if e != nil {
			output["reason"] = e.Error()
		}
		if json.NewEncoder(out).Encode(output) != nil || e != nil {
			return 1
		}
		return 0
	}
	if operation == "user-input-interrupt" {
		result, e := c.ProbeUserInputInterrupted(ctx, m.ThreadID, m.Workspace)
		verified := false
		if e == nil && result.Interrupted {
			t, readErr := reader.ReadFull(ctx, m.ThreadID)
			if readErr != nil {
				e = readErr
			} else {
				e = probe.VerifyInterruptedTurn(t.Turns, result.TurnID)
				verified = e == nil
			}
		}
		output := map[string]any{"userInputInterrupt": result, "persistedExactTurnInterrupted": verified}
		if e != nil {
			output["reason"] = e.Error()
		}
		if json.NewEncoder(out).Encode(output) != nil || e != nil {
			return 1
		}
		return 0
	}
	if operation == "command-cancel" {
		result, err := c.ProbeCommandCancel(ctx, m.ThreadID, m.Workspace)
		verified := false
		commandItemInHistory := false
		if err == nil && !result.CleanupInterrupted {
			verifyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			for {
				t, e := reader.ReadFull(verifyCtx, m.ThreadID)
				if e != nil {
					err = e
					break
				}
				commandItemInHistory = probe.CommandItemInHistory(t.Turns, result.TurnID, result.ItemID)
				err = probe.VerifyCancelledCommand(t.Turns, result.TurnID, result.ItemID)
				if err == nil {
					verified = true
					break
				}
				select {
				case <-time.After(200 * time.Millisecond):
				case <-verifyCtx.Done():
					goto cancelDone
				}
			}
		}
	cancelDone:
		output := map[string]any{"commandCancel": result, "persistedExactTurnInterrupted": verified, "commandItemInPublicHistory": commandItemInHistory}
		if err != nil {
			output["reason"] = err.Error()
		}
		if json.NewEncoder(out).Encode(output) != nil || err != nil {
			return 1
		}
		return 0
	}
	if operation == "command-decline" {
		result, err := c.ProbeCommandDecline(ctx, m.ThreadID, m.Workspace)
		verified := false
		if err == nil {
			verifyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			for {
				t, e := reader.ReadFull(verifyCtx, m.ThreadID)
				if e != nil {
					err = e
					break
				}
				err = probe.VerifyDeclinedCommand(t.Turns, result.TurnID, result.ItemID)
				if err == nil {
					verified = true
					break
				}
				select {
				case <-time.After(200 * time.Millisecond):
				case <-verifyCtx.Done():
					goto declineDone
				}
			}
		}
	declineDone:
		output := map[string]any{"commandDecline": result, "persistedExactDeclineVerified": verified}
		if err != nil {
			output["reason"] = err.Error()
		}
		if json.NewEncoder(out).Encode(output) != nil || err != nil {
			return 1
		}
		return 0
	}
	if operation == "user-input" {
		result, err := c.ProbeUserInput(ctx, m.ThreadID, m.Workspace)
		verified := false
		if err == nil {
			verifyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			defer stop()
			for {
				t, e := reader.ReadFull(verifyCtx, m.ThreadID)
				if e != nil {
					err = e
					break
				}
				err = probe.VerifyCompletedReply(t.Turns, result.TurnID, result.Marker)
				if err == nil {
					verified = true
					break
				}
				select {
				case <-time.After(200 * time.Millisecond):
				case <-verifyCtx.Done():
					goto inputDone
				}
			}
		}
	inputDone:
		output := map[string]any{"userInput": result, "persistedExactTurnVerified": verified}
		if err != nil {
			output["reason"] = err.Error()
		}
		if json.NewEncoder(out).Encode(output) != nil || err != nil {
			return 1
		}
		return 0
	}
	if operation == "stale-interaction" {
		result, err := c.ProbeStaleInteraction(ctx, m.ThreadID, m.Workspace)
		if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		if json.NewEncoder(out).Encode(result) != nil {
			return 1
		}
		return 0
	}
	result, err := c.ExerciseFixture(ctx, m.ThreadID, m.Workspace)
	verified := false
	if err == nil {
		verifyCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			thread, readErr := reader.ReadFull(verifyCtx, m.ThreadID)
			if readErr != nil {
				err = readErr
				break
			}
			err = probe.VerifyExercise(thread.Turns, result.ReplyTurnID, result.InterruptedTurnID, result.ReplyMarker)
			if err == nil {
				verified = true
				break
			}
			select {
			case <-ticker.C:
			case <-verifyCtx.Done():
				goto verificationDone
			}
		}
	}
verificationDone:
	output := map[string]any{"liveReplyObserved": result.LiveReplyObserved, "interruptAccepted": result.InterruptAccepted, "persistedExactTurnsVerified": verified, "snapshots": result.SnapshotCount, "patchBatches": result.PatchBatches}
	if err != nil {
		output["reason"] = err.Error()
	}
	if json.NewEncoder(out).Encode(output) != nil || err != nil {
		return 1
	}
	return 0
}
