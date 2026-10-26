// Package modelsettings defines Ariel's provider-neutral execution options.
// Native RPC names and parameter mappings must remain in the Codex adapters.
package modelsettings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
)

var ErrInvalid = errors.New("INVALID_ARGUMENT")
var ErrConflict = errors.New("SETTINGS_CONFLICT")
var ErrCapacity = errors.New("OVERLOADED")

type Effort struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type Tier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Model struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Efforts         []Effort `json:"efforts"`
	DefaultEffort   string   `json:"defaultEffort"`
	Tiers           []Tier   `json:"tiers"`
	DefaultTier     *string  `json:"defaultTier"`
	InputModalities []string `json:"inputModalities"`
	IsDefault       bool     `json:"isDefault"`
}
type Catalog struct {
	Models []Model `json:"models"`
}
type Speed struct {
	Mode   string `json:"mode"`
	TierID string `json:"tierId,omitempty"`
}
type Values struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Speed  Speed  `json:"speed"`
}
type Patch struct {
	Model  *string `json:"model,omitempty"`
	Effort *string `json:"effort,omitempty"`
	Speed  *Speed  `json:"speed,omitempty"`
}
type Snapshot struct {
	Revision  string `json:"revision"`
	Values    Values `json:"values"`
	Freshness string `json:"freshness"`
	Writable  bool   `json:"writable"`
}

func (p Patch) Apply(v Values) Values {
	if p.Model != nil {
		v.Model = *p.Model
	}
	if p.Effort != nil {
		v.Effort = *p.Effort
	}
	if p.Speed != nil {
		v.Speed = *p.Speed
	}
	return v
}
func Revision(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (c Catalog) Validate(v Values, images bool) error {
	for _, m := range c.Models {
		if m.ID != v.Model {
			continue
		}
		if images && !slices.Contains(m.InputModalities, "image") {
			return ErrInvalid
		}
		if v.Effort != "" && !slices.ContainsFunc(m.Efforts, func(e Effort) bool { return e.ID == v.Effort }) {
			return ErrInvalid
		}
		switch v.Speed.Mode {
		case "inherit", "standard":
			if v.Speed.TierID != "" {
				return ErrInvalid
			}
		case "tier":
			if !slices.ContainsFunc(m.Tiers, func(t Tier) bool { return t.ID == v.Speed.TierID }) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		return nil
	}
	return ErrInvalid
}
func (c Catalog) CheckBounds() error {
	if len(c.Models) > 512 {
		return ErrCapacity
	}
	seen := map[string]bool{}
	valid := func(s string, n int) bool { return len(s) <= n }
	for _, m := range c.Models {
		if m.ID == "" || seen[m.ID] || !valid(m.ID, 256) || !valid(m.Name, 256) || !valid(m.Description, 2048) || len(m.Efforts) > 32 || len(m.Tiers) > 16 || len(m.InputModalities) > 8 {
			return ErrCapacity
		}
		seen[m.ID] = true
		for _, e := range m.Efforts {
			if e.ID == "" || !valid(e.ID, 128) || !valid(e.Description, 2048) {
				return ErrCapacity
			}
		}
		for _, t := range m.Tiers {
			if t.ID == "" || !valid(t.ID, 128) || !valid(t.Name, 256) || !valid(t.Description, 2048) {
				return ErrCapacity
			}
		}
	}
	b, err := json.Marshal(c)
	if err != nil || len(b) > 1<<20 {
		return ErrCapacity
	}
	return nil
}
