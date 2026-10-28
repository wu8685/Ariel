package desktopipc

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wu8685/Ariel/protocol"
)

type AsyncQuestionTurn struct {
	ID, Status string
	Items      []json.RawMessage
	Partial    bool // item pagination cannot prove absence of later answers
}

type nativeAsyncQuestion struct {
	Title   string   `json:"title"`
	Options []string `json:"options,omitempty"`
}
type nativeAsyncAnswer struct {
	QuestionID string `json:"questionItemId"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`
}

func nativeAsyncQuestionID(itemID string, index int) string {
	raw, _ := json.Marshal([]any{"request_user_input_async", itemID, index})
	return string(raw)
}

func strictQuestionJSON(raw []byte, out any) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}

// ProjectAsyncQuestions only inspects the supplied turns/page. It never reads
// history, creates a follower, or infers questions from assistant prose.
func ProjectAsyncQuestions(threadID string, turns []AsyncQuestionTurn) protocol.AsyncQuestionSnapshot {
	out := protocol.AsyncQuestionSnapshot{Version: 1, Questions: []protocol.AsyncQuestion{}, Warnings: []protocol.AsyncQuestionWarning{}}
	for _, turn := range turns {
		byNativeID := map[string]int{}
		warn := func(itemID, code string) {
			if len(out.Warnings) < 64 {
				out.Warnings = append(out.Warnings, protocol.AsyncQuestionWarning{TurnID: turn.ID, ItemID: itemID, Code: code})
			}
		}
		for _, raw := range turn.Items {
			var item struct {
				ID, Type, Status string
				TargetTurnID     string          `json:"targetTurnId"`
				Questions        json.RawMessage `json:"questions"`
				Content, Input   []struct{ Type, Text string }
			}
			if json.Unmarshal(raw, &item) != nil {
				continue
			}
			if item.Type == "agentMessage" && len(item.Questions) > 0 && string(item.Questions) != "null" {
				var questions []json.RawMessage
				if json.Unmarshal(item.Questions, &questions) != nil || len(questions) > 16 || item.ID == "" {
					warn(item.ID, "UNSUPPORTED_QUESTION")
					continue
				}
				for index, rawQuestion := range questions {
					var q nativeAsyncQuestion
					if !strictQuestionJSON(rawQuestion, &q) || strings.TrimSpace(q.Title) == "" || utf8.RuneCountInString(q.Title) > 2048 || len(q.Options) > 32 {
						warn(item.ID, "UNSUPPORTED_QUESTION")
						continue
					}
					valid := true
					for _, option := range q.Options {
						if strings.TrimSpace(option) == "" || utf8.RuneCountInString(option) > 512 {
							valid = false
						}
					}
					if !valid {
						warn(item.ID, "UNSUPPORTED_QUESTION")
						continue
					}
					if len(out.Questions) >= 64 {
						warn(item.ID, "QUESTION_CAPACITY")
						continue
					}
					if q.Options == nil {
						q.Options = []string{}
					}
					nativeID := nativeAsyncQuestionID(item.ID, index)
					identity, _ := json.Marshal([]string{threadID, turn.ID, nativeID})
					fingerprint, _ := json.Marshal([]any{nativeID, q.Title, q.Options})
					status := "ended"
					if turn.Partial {
						status = "unverified"
					}
					if turn.Status == "inProgress" {
						status = "open"
					}
					byNativeID[nativeID] = len(out.Questions)
					out.Questions = append(out.Questions, protocol.AsyncQuestion{ID: fmt.Sprintf("aq-%x", sha256.Sum256(identity)), Fingerprint: fmt.Sprintf("%x", sha256.Sum256(fingerprint)), TurnID: turn.ID, ItemID: item.ID, Index: index, Title: q.Title, Options: q.Options, Status: status})
				}
			}
			var text string
			if item.Type == "userMessage" && len(item.Content) == 1 && item.Content[0].Type == "text" {
				text = item.Content[0].Text
			}
			if item.Type == "steeringUserMessage" && item.Status == "accepted" && item.TargetTurnID == turn.ID && len(item.Input) == 1 && item.Input[0].Type == "text" {
				text = item.Input[0].Text
			}
			for _, answer := range parseAsyncAnswers(text) {
				if index, ok := byNativeID[answer.QuestionID]; ok && out.Questions[index].Title == answer.Question && strings.TrimSpace(answer.Answer) != "" && utf8.RuneCountInString(answer.Answer) <= 8192 {
					out.Questions[index].Status = "answered"
					out.Questions[index].Answer = answer.Answer
				}
			}
		}
	}
	// Reserve room for explicit warnings, and never partially display a huge
	// question as if it were complete. Byte limits include UTF-8 JSON escaping.
	for {
		raw, _ := json.Marshal(out)
		if len(raw) <= 64<<10 {
			break
		}
		if len(out.Questions) > 0 {
			last := out.Questions[len(out.Questions)-1]
			out.Questions = out.Questions[:len(out.Questions)-1]
			if len(out.Warnings) < 64 {
				out.Warnings = append(out.Warnings, protocol.AsyncQuestionWarning{TurnID: last.TurnID, ItemID: last.ItemID, Code: "QUESTION_CAPACITY"})
			}
		} else {
			out.Warnings = out.Warnings[:len(out.Warnings)-1]
		}
	}
	return out
}

func parseAsyncAnswers(text string) []nativeAsyncAnswer {
	text = strings.TrimSpace(text)
	const start = "<send_user_message_question_reply>"
	const end = "</send_user_message_question_reply>"
	if len(text) > 64<<10 || !strings.HasPrefix(text, start) || !strings.HasSuffix(text, end) {
		return nil
	}
	raw := []byte(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, start), end)))
	var answers []nativeAsyncAnswer
	if !strictQuestionJSON(raw, &answers) {
		var answer nativeAsyncAnswer
		if !strictQuestionJSON(raw, &answer) {
			return nil
		}
		answers = []nativeAsyncAnswer{answer}
	}
	if len(answers) > 64 {
		return nil
	}
	return answers
}
