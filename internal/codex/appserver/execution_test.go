package appserver

import (
	"testing"

	"github.com/wu8685/Ariel/internal/modelsettings"
)

func TestExecutionParamsDistinguishStandardAndInheritance(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		expected any
	}{{"standard", "default"}, {"inherit", nil}, {"tier", "priority"}} {
		speed := modelsettings.Speed{Mode: tc.mode}
		if tc.mode == "tier" {
			speed.TierID = "priority"
		}
		p := ExecutionParams(modelsettings.Values{Model: "m", Effort: "high", Speed: speed})
		if p["model"] != "m" || p["effort"] != "high" || p["serviceTier"] != tc.expected || len(p) != 3 {
			t.Fatal(p)
		}
	}
}
