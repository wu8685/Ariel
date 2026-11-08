package puck

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wu8685/Ariel/internal/puck/toolcontract"
)

const unrecognizedQuestionText = "助手发来了一个无法识别的问题"

type QuestionCard struct {
	QuestionID  string                        `json:"questionId,omitempty"`
	Questions   []toolcontract.Question       `json:"questions"`
	CardState   string                        `json:"cardState,omitempty"`
	Answers     []QuestionAnswer              `json:"answers,omitempty"`
	Question    string                        `json:"question,omitempty"`
	Options     []toolcontract.QuestionOption `json:"options,omitempty"`
	AllowCustom bool                          `json:"allowCustom,omitempty"`
	MultiSelect bool                          `json:"multiSelect,omitempty"`
}

type QuestionAnswer struct {
	QuestionID string   `json:"questionId"`
	Values     []string `json:"values,omitempty"`
	Custom     string   `json:"custom,omitempty"`
}

func normalizeQuestionCard(raw json.RawMessage) (QuestionCard, string, error) {
	request, details := toolcontract.DecodeAskUser(raw)
	if len(details) != 0 {
		return QuestionCard{}, unrecognizedQuestionText, fmt.Errorf("invalid ask_user payload: %s", details[0].Field)
	}
	card := QuestionCard{Questions: request.Questions, CardState: "pending"}
	text := request.Questions[0].Question
	if len(request.Questions) == 1 {
		question := request.Questions[0]
		card.Question, card.Options = question.Question, question.Options
		card.AllowCustom, card.MultiSelect = question.AllowCustom, question.MultiSelect
	} else {
		text = fmt.Sprintf("请回答以下 %d 个问题", len(request.Questions))
	}
	return card, text, nil
}

func validateQuestionAnswers(card QuestionCard, answers []QuestionAnswer) []toolcontract.FieldError {
	if len(answers) != len(card.Questions) {
		return []toolcontract.FieldError{{Field: "answers", Reason: "every question requires one answer", Expected: "one answer per question"}}
	}
	byID := make(map[string]toolcontract.Question, len(card.Questions))
	for _, question := range card.Questions {
		byID[question.ID] = question
	}
	seen := map[string]struct{}{}
	for index, answer := range answers {
		question, ok := byID[answer.QuestionID]
		if !ok {
			return []toolcontract.FieldError{{Field: fmt.Sprintf("answers[%d].questionId", index), Reason: "unknown question id", Expected: "id from card questions"}}
		}
		if _, duplicate := seen[answer.QuestionID]; duplicate {
			return []toolcontract.FieldError{{Field: fmt.Sprintf("answers[%d].questionId", index), Reason: "duplicate question id", Expected: "unique question id"}}
		}
		seen[answer.QuestionID] = struct{}{}
		if !question.MultiSelect && len(answer.Values) > 1 {
			return []toolcontract.FieldError{{Field: fmt.Sprintf("answers[%d].values", index), Reason: "multiple values are not allowed", Expected: "at most one option"}}
		}
		allowed := map[string]struct{}{}
		for _, option := range question.Options {
			allowed[option.Value] = struct{}{}
		}
		values := map[string]struct{}{}
		for _, value := range answer.Values {
			if _, ok := allowed[value]; !ok {
				return []toolcontract.FieldError{{Field: fmt.Sprintf("answers[%d].values", index), Reason: "unknown option value", Expected: "value from card options"}}
			}
			if _, duplicate := values[value]; duplicate {
				return []toolcontract.FieldError{{Field: fmt.Sprintf("answers[%d].values", index), Reason: "duplicate option value", Expected: "unique values"}}
			}
			values[value] = struct{}{}
		}
		if strings.TrimSpace(answer.Custom) != "" && !question.AllowCustom {
			return []toolcontract.FieldError{{Field: fmt.Sprintf("answers[%d].custom", index), Reason: "custom answer is not allowed", Expected: "empty value"}}
		}
		if len(answer.Values) == 0 && strings.TrimSpace(answer.Custom) == "" {
			return []toolcontract.FieldError{{Field: fmt.Sprintf("answers[%d]", index), Reason: "answer is empty", Expected: "selected option or custom text"}}
		}
	}
	return nil
}
