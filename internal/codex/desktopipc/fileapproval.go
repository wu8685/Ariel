package desktopipc

import "encoding/json"

// FileChangeItem returns only an item referenced by the current canonical
// history (or the verified legacy turns form), never an orphaned entity.
func FileChangeItem(state json.RawMessage, turnID, itemID string) (json.RawMessage, error) {
	if turnID == "" || itemID == "" {
		return nil, ErrProtocol
	}
	var snapshot struct {
		Turns []struct {
			TurnID string            `json:"turnId"`
			Items  []json.RawMessage `json:"items"`
		} `json:"turns"`
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]struct {
					TurnID string            `json:"turnId"`
					Items  []json.RawMessage `json:"items"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &snapshot) != nil {
		return nil, ErrProtocol
	}
	turns := snapshot.Turns
	if snapshot.TurnHistory.Kind == "canonical" {
		turns = nil
		for _, island := range snapshot.TurnHistory.History.Islands {
			for _, entry := range island.Entries {
				turn, ok := snapshot.TurnHistory.History.Entities[entry.Value]
				if !ok {
					return nil, ErrProtocol
				}
				turns = append(turns, turn)
			}
		}
	} else if snapshot.TurnHistory.Kind != "" {
		return nil, ErrProtocol
	}
	var found json.RawMessage
	for _, turn := range turns {
		if turn.TurnID != turnID {
			continue
		}
		for _, raw := range turn.Items {
			var item struct {
				ID      string            `json:"id"`
				Type    string            `json:"type"`
				Changes []json.RawMessage `json:"changes"`
			}
			if json.Unmarshal(raw, &item) == nil && item.ID == itemID {
				if item.Type != "fileChange" || len(item.Changes) == 0 || found != nil {
					return nil, ErrProtocol
				}
				found = raw
			}
		}
	}
	if found == nil {
		return nil, ErrProtocol
	}
	return found, nil
}
