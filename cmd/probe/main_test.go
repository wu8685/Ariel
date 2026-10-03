package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCLIRejectsIncompleteOrUnknownOperations(t *testing.T) {
	for _, args := range [][]string{{}, {"turn.start"}, {"owner"}, {"history", "--unknown"}, {"fixture"}, {"owner", "--thread", "x", "--watch", "-1s"}} {
		var out, errout bytes.Buffer
		if code := run(context.Background(), args, &out, &errout); code != 2 {
			t.Fatalf("%v returned %d: %s", args, code, errout.String())
		}
		if out.Len() != 0 {
			t.Fatalf("%v wrote partial output", args)
		}
	}
}

func TestCLIHelpExplainsReadOnlyBoundary(t *testing.T) {
	var out, errout bytes.Buffer
	if code := run(context.Background(), []string{"help"}, &out, &errout); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "read-only") || !strings.Contains(out.String(), "--thread") {
		t.Fatal(out.String())
	}
}

func TestCLIWriteOperationsRequireExplicitFlagAndManifest(t *testing.T) {
	for _, args := range [][]string{{"fixture", "create"}, {"fixture", "seed"}, {"fixture", "seed", "--write-enabled"}, {"fixture", "create", "--write-enabled"}} {
		var out, errout bytes.Buffer
		if code := run(context.Background(), args, &out, &errout); code != 2 {
			t.Fatalf("%v returned %d", args, code)
		}
	}
}

func TestCLIExerciseRequiresWriteFlagBeforeOpeningManifest(t *testing.T) {
	var out, errout bytes.Buffer
	if code := run(context.Background(), []string{"fixture", "exercise", "--manifest", "/missing"}, &out, &errout); code != 2 {
		t.Fatalf("code %d", code)
	}
}
