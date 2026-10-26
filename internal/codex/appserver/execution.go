package appserver

import "github.com/wu8685/Ariel/internal/modelsettings"

// ExecutionParams is deliberately allowlisted: no permissions or mode fields.
func ExecutionParams(v modelsettings.Values) map[string]any {
	var tier any
	switch v.Speed.Mode {
	case "standard":
		tier = "default"
	case "tier":
		tier = v.Speed.TierID
	}
	var effort any
	if v.Effort != "" {
		effort = v.Effort
	}
	return map[string]any{"model": v.Model, "effort": effort, "serviceTier": tier}
}
