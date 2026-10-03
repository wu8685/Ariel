package probe

import (
	"encoding/json"
	"errors"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

func VerifyCompletedReply(turns []appserver.Turn, id, marker string) error {
	if id == "" || marker == "" {
		return errors.New("missing reply identity or marker")
	}
	for _, turn := range turns {
		if turn.ID == id && turn.Status == "completed" {
			for _, body := range turn.Items {
				var item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if json.Unmarshal(body, &item) == nil && item.Type == "agentMessage" && item.Text == marker {
					return nil
				}
			}
		}
	}
	return errors.New("persisted history does not confirm exact completed reply")
}

func VerifyInterruptedTurn(turns []appserver.Turn, id string) error {
	if id == "" {
		return errors.New("missing turn identity")
	}
	for _, turn := range turns {
		if turn.ID == id && turn.Status == "interrupted" {
			return nil
		}
	}
	return errors.New("persisted history does not confirm exact interrupted turn")
}

func VerifyDeclinedCommand(turns []appserver.Turn, turnID, itemID string) error {
	if turnID != "" && itemID != "" {
		for _, turn := range turns {
			if turn.ID == turnID {
				for _, raw := range turn.Items {
					var item struct {
						ID     string `json:"id"`
						Type   string `json:"type"`
						Status string `json:"status"`
					}
					if json.Unmarshal(raw, &item) == nil && item.ID == itemID && item.Type == "commandExecution" && item.Status == "declined" {
						return nil
					}
				}
			}
		}
	}
	return errors.New("persisted history does not confirm exact declined command item")
}

func VerifyCancelledCommand(turns []appserver.Turn, turnID, itemID string) error {
	if turnID == "" || itemID == "" {
		return errors.New("missing cancelled command identity")
	}
	for _, turn := range turns {
		if turn.ID == turnID && turn.Status == "interrupted" {
			for _, raw := range turn.Items {
				var item struct {
					ID     string `json:"id"`
					Type   string `json:"type"`
					Status string `json:"status"`
				}
				if json.Unmarshal(raw, &item) == nil && item.ID == itemID && item.Type == "commandExecution" && item.Status == "declined" {
					return nil
				}
				if item.ID == itemID && item.Type == "commandExecution" {
					return errors.New("command item does not show declined status")
				}
			}
			// Nested exec_command approvals are absent from public thread/read items.
			// The exact persisted turn is still authoritative for the stop outcome.
			return nil
		}
	}
	return errors.New("persisted history does not confirm exact interrupted turn")
}

func CommandItemInHistory(turns []appserver.Turn, turnID, itemID string) bool {
	if turnID == "" || itemID == "" {
		return false
	}
	for _, turn := range turns {
		if turn.ID == turnID {
			for _, raw := range turn.Items {
				var item struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				}
				if json.Unmarshal(raw, &item) == nil && item.ID == itemID && item.Type == "commandExecution" {
					return true
				}
			}
		}
	}
	return false
}

func VerifyExercise(turns []appserver.Turn, replyID, stoppedID, marker string) error {
	if replyID == "" || stoppedID == "" || replyID == stoppedID || marker == "" {
		return errors.New("missing exact exercise identifiers")
	}
	reply, stopped := false, false
	for _, turn := range turns {
		if turn.ID == replyID && turn.Status == "completed" {
			for _, body := range turn.Items {
				var item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				json.Unmarshal(body, &item)
				if item.Type == "agentMessage" && item.Text == marker {
					reply = true
				}
			}
		}
		if turn.ID == stoppedID && turn.Status == "interrupted" {
			stopped = true
		}
	}
	if !reply || !stopped {
		return errors.New("persisted history does not yet confirm exact completed reply and interrupted turn")
	}
	return nil
}
