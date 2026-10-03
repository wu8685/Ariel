package desktopipc

import "encoding/json"

// TurnContainsClientMessage verifies the native userMessage.clientId field.
// The native item.id is a different, owner-generated identifier.
func TurnContainsClientMessage(state json.RawMessage, turnID, clientMessageID, text string) bool {
	if turnID == "" || clientMessageID == "" || text == "" {
		return false
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
		return false
	}
	var items []json.RawMessage
	switch snapshot.TurnHistory.Kind {
	case "canonical":
		for _, island := range snapshot.TurnHistory.History.Islands {
			for _, entry := range island.Entries {
				turn, ok := snapshot.TurnHistory.History.Entities[entry.Value]
				if !ok {
					return false
				}
				if turn.TurnID == turnID {
					items = turn.Items
				}
			}
		}
	case "":
		for _, turn := range snapshot.Turns {
			if turn.TurnID == turnID {
				items = turn.Items
			}
		}
	default:
		return false
	}
	for _, raw := range items {
		var item struct {
			Type     string `json:"type"`
			ClientID string `json:"clientId"`
			Content  []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type != "userMessage" || item.ClientID != clientMessageID || len(item.Content) != 1 {
			continue
		}
		if item.Content[0].Type == "text" && item.Content[0].Text == text {
			return true
		}
	}
	return false
}
