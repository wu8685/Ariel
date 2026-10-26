package modelsettings

import (
	"encoding/json"
	"testing"
)

func TestCatalogValidatesCombinationsWithoutHardcodedModels(t *testing.T) {
	c := Catalog{Models: []Model{{ID: "future-model", Name: "Future", Efforts: []Effort{{ID: "new-effort", Description: "deep"}}, DefaultEffort: "new-effort", Tiers: []Tier{{ID: "priority-native", Name: "Fast"}}, InputModalities: []string{"text"}}}}
	v := Values{Model: "future-model", Effort: "new-effort", Speed: Speed{Mode: "tier", TierID: "priority-native"}}
	if err := c.Validate(v, false); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Values{{Model: "missing"}, {Model: "future-model", Effort: "unsupported"}, {Model: "future-model", Effort: "new-effort", Speed: Speed{Mode: "tier", TierID: "fast"}}} {
		if c.Validate(invalid, false) == nil {
			t.Fatalf("accepted invalid: %+v", invalid)
		}
	}
	if c.Validate(v, true) == nil {
		t.Fatal("text-only model accepted images")
	}
}

func TestPatchSeparatesOmittedStandardAndInheritance(t *testing.T) {
	before := Values{Model: "m", Effort: "high", Speed: Speed{Mode: "tier", TierID: "priority"}}
	for _, tc := range []struct {
		body string
		mode string
	}{{`{}`, "tier"}, {`{"speed":{"mode":"standard"}}`, "standard"}, {`{"speed":{"mode":"inherit"}}`, "inherit"}} {
		var p Patch
		if err := json.Unmarshal([]byte(tc.body), &p); err != nil {
			t.Fatal(err)
		}
		after := p.Apply(before)
		if after.Model != "m" || after.Effort != "high" || after.Speed.Mode != tc.mode {
			t.Fatalf("patch = %+v", after)
		}
	}
}
