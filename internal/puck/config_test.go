package puck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateAvailabilityDoesNotTouchStorageWhenOffOrIneligible(t *testing.T) {
	parent := t.TempDir()
	for _, tc := range []struct {
		name, auth, origin, state, reason, detail string
		enabled                                   bool
	}{
		{name: "off", auth: "passkey", origin: "https://example.test", state: StateOff},
		{name: "pin", enabled: true, auth: "pin", origin: "http://localhost", state: StateUnavailable, reason: ReasonRequiresPasskeyHTTPS, detail: "pin_mode"},
		{name: "http", enabled: true, auth: "passkey", origin: "http://example.test", state: StateUnavailable, reason: ReasonRequiresPasskeyHTTPS, detail: "public_origin_not_https"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := filepath.Join(parent, tc.name, "puck")
			got := Evaluate(Config{Enabled: tc.enabled, WebAuth: tc.auth, PublicOrigin: tc.origin, DataDir: dataDir})
			defer got.Close()
			if got.State != tc.state || got.Reason != tc.reason || got.Detail != tc.detail {
				t.Fatalf("got %+v", got)
			}
			if _, err := os.Stat(filepath.Dir(dataDir)); !os.IsNotExist(err) {
				t.Fatal("ineligible Puck touched its storage parent")
			}
		})
	}
}

func TestEvaluateAvailabilityReadyAndExclusiveLock(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "puck")
	first := Evaluate(Config{Enabled: true, WebAuth: "passkey", PublicOrigin: "https://example.test", DataDir: dataDir})
	defer first.Close()
	if first.State != StateReady || first.Lock == nil {
		t.Fatalf("first availability: %+v", first)
	}
	second := Evaluate(Config{Enabled: true, WebAuth: "passkey", PublicOrigin: "https://example.test", DataDir: dataDir})
	defer second.Close()
	if second.State != StateUnavailable || second.Reason != ReasonDataDirLocked || second.Detail != "data_dir_locked" {
		t.Fatalf("second availability: %+v", second)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third := Evaluate(Config{Enabled: true, WebAuth: "passkey", PublicOrigin: "https://example.test", DataDir: dataDir})
	defer third.Close()
	if third.State != StateReady {
		t.Fatalf("lock not released: %+v", third)
	}
}

func TestEvaluateAvailabilityRejectsUnwritableTargetWithoutReplacingIt(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "puck")
	if err := os.WriteFile(target, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	got := Evaluate(Config{Enabled: true, WebAuth: "passkey", PublicOrigin: "https://example.test", DataDir: target})
	defer got.Close()
	if got.State != StateUnavailable || got.Reason != ReasonDataDirUnwritable || got.Detail != "data_dir_unwritable" {
		t.Fatalf("got %+v", got)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "not a directory" {
		t.Fatal("target was replaced or modified")
	}
}
