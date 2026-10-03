package probe

import (
	"errors"
	"testing"
)

func TestBinarySelectionMatchesDesktopAndHonorsExplicitPath(t *testing.T) {
	versions := map[string]string{"/bundle/codex": "0.160.0", "/chosen/codex": "0.160.0", "/path/codex": "0.145.0"}
	read := func(path string) (string, error) {
		if v, ok := versions[path]; ok {
			return v, nil
		}
		return "", errors.New("missing")
	}
	for _, tc := range []struct {
		explicit, want string
		fail           bool
	}{
		{"", "/bundle/codex", false}, {"/chosen/codex", "/chosen/codex", false}, {"/path/codex", "", true}, {"/missing", "", true},
	} {
		got, _, err := SelectBinary(tc.explicit, "/bundle/codex", read)
		if (err != nil) != tc.fail || got != tc.want {
			t.Fatalf("explicit %q: got %q %v", tc.explicit, got, err)
		}
	}
	if _, _, err := SelectBinary("", "/missing", read); err == nil {
		t.Fatal("missing bundle silently substituted")
	}
}

func TestIPCProfileRequiresMatchingDesktopBuildAndCLI(t *testing.T) {
	for _, tc := range []struct {
		desktop, cli string
		ok           bool
	}{
		{"26.930.31730", "0.160.0", true},
		{"26.931.1", "0.160.0", false},
		{"26.930.31730", "0.161.0", false},
		{"", "0.160.0", false},
	} {
		if got := IPCProfileVerified(tc.desktop, tc.cli); got != tc.ok {
			t.Fatalf("%+v: %v", tc, got)
		}
	}
}
