package relay

import (
	"context"
	"encoding/json"

	"github.com/wu8685/Ariel/protocol"
)

type questionTouch struct {
	key      cacheKey
	watching bool
}

func (s *Server) negotiateQuestionResponse(ctx context.Context, agent *peer, id string, reply map[string]any) {
	s.mu.Lock()
	touch, exists := agent.questionTouches[id]
	delete(agent.questionTouches, id)
	data, _ := reply["data"].(map[string]any)
	demand := false
	if entry := s.cache.entries[touch.key]; entry != nil {
		for _, watcher := range entry.watchers {
			demand = demand || watcher.questions
		}
	}
	enable := exists && demand && s.agents[agent.deviceID] == agent && !agent.asyncQuestions && reply["outcome"] == "accepted" && data["asyncQuestionsVersion"] == float64(1)
	if enable {
		agent.asyncQuestions = true
	}
	s.mu.Unlock()
	if enable {
		_ = s.sendCacheTouch(ctx, agent, touch.key, touch.watching, newRequestID())
	}
}

func validatedQuestionSidecar(agent *peer, raw, thread json.RawMessage) json.RawMessage {
	if !agent.asyncQuestions || len(raw) == 0 {
		return nil
	}
	snapshot, ok := protocol.DecodeAsyncQuestions(raw)
	if !ok {
		return json.RawMessage(`{"version":1,"writable":false,"questions":[],"warnings":[{"turnId":"","itemId":"","code":"INVALID_QUESTION_DATA"}]}`)
	}
	var source struct {
		Turns []struct {
			TurnID string `json:"turnId"`
			Items  []struct {
				ItemID string `json:"itemId"`
			} `json:"items"`
		} `json:"turns"`
	}
	if json.Unmarshal(thread, &source) != nil {
		return nil
	}
	known := map[string]bool{}
	for _, turn := range source.Turns {
		for _, item := range turn.Items {
			known[turn.TurnID+"\x00"+item.ItemID] = true
		}
	}
	for _, q := range snapshot.Questions {
		if !known[q.TurnID+"\x00"+q.ItemID] {
			return json.RawMessage(`{"version":1,"writable":false,"questions":[],"warnings":[{"turnId":"","itemId":"","code":"INVALID_QUESTION_DATA"}]}`)
		}
	}
	return append(json.RawMessage(nil), raw...)
}
