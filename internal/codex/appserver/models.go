package appserver

import (
	"context"

	"github.com/wu8685/Ariel/internal/modelsettings"
)

type ModelReader struct{ RPC RPC }

func (r ModelReader) Models(ctx context.Context) (modelsettings.Catalog, error) {
	catalog := modelsettings.Catalog{Models: []modelsettings.Model{}}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 8; page++ {
		var out struct {
			Data []struct {
				ID          string `json:"model"`
				Name        string `json:"displayName"`
				Description string `json:"description"`
				Hidden      bool   `json:"hidden"`
				Efforts     []struct {
					ID          string `json:"reasoningEffort"`
					Description string `json:"description"`
				} `json:"supportedReasoningEfforts"`
				DefaultEffort   string               `json:"defaultReasoningEffort"`
				Tiers           []modelsettings.Tier `json:"serviceTiers"`
				DefaultTier     *string              `json:"defaultServiceTier"`
				InputModalities []string             `json:"inputModalities"`
				IsDefault       bool                 `json:"isDefault"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		p := map[string]any{"includeHidden": false, "limit": 100}
		if cursor != "" {
			p["cursor"] = cursor
		}
		if err := r.RPC.Call(ctx, "model/list", p, &out); err != nil {
			return catalog, err
		}
		for _, m := range out.Data {
			if m.Hidden {
				continue
			}
			efforts := make([]modelsettings.Effort, 0, len(m.Efforts))
			for _, e := range m.Efforts {
				efforts = append(efforts, modelsettings.Effort{ID: e.ID, Description: e.Description})
			}
			if m.Tiers == nil {
				m.Tiers = []modelsettings.Tier{}
			}
			if m.InputModalities == nil {
				m.InputModalities = []string{}
			}
			catalog.Models = append(catalog.Models, modelsettings.Model{ID: m.ID, Name: m.Name, Description: m.Description, Efforts: efforts, DefaultEffort: m.DefaultEffort, Tiers: m.Tiers, DefaultTier: m.DefaultTier, InputModalities: m.InputModalities, IsDefault: m.IsDefault})
		}
		if err := catalog.CheckBounds(); err != nil {
			return modelsettings.Catalog{}, err
		}
		if out.NextCursor == nil || *out.NextCursor == "" {
			return catalog, nil
		}
		cursor = *out.NextCursor
		if seen[cursor] {
			return modelsettings.Catalog{}, ErrProtocol
		}
		seen[cursor] = true
	}
	return modelsettings.Catalog{}, modelsettings.ErrCapacity
}
