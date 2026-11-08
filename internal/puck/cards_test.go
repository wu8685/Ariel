package puck

import (
	"encoding/json"
	"testing"
)

func TestNormalizeQuestionCardSupportsSingleAndQuestionsArray(t *testing.T) {
	for _, raw := range []string{
		`{"question":"部署到哪里？","options":[{"label":"测试"}]}`,
		`{"questions":[{"id":"entry","question":"从哪里进入？","options":[{"label":"Desktop","description":"桌面应用"}]}]}`,
	} {
		card, text, err := normalizeQuestionCard(json.RawMessage(raw))
		if err != nil || text == "" || len(card.Questions) != 1 || card.Questions[0].ID == "" || card.Questions[0].Options[0].Value == "" {
			t.Fatalf("card=%+v text=%q err=%v", card, text, err)
		}
	}
}

func TestNormalizeQuestionCardUsesFriendlyFallback(t *testing.T) {
	card, text, err := normalizeQuestionCard(json.RawMessage(`{"questions":[]}`))
	if err == nil || text != unrecognizedQuestionText || len(card.Questions) != 0 {
		t.Fatalf("card=%+v text=%q err=%v", card, text, err)
	}
}

func TestValidateQuestionAnswersPreservesQuestionIDs(t *testing.T) {
	card, _, err := normalizeQuestionCard(json.RawMessage(`{"questions":[{"id":"entry","question":"入口？","options":[{"label":"Desktop","value":"desktop"}]},{"id":"features","question":"能力？","options":[{"label":"浏览器","value":"browser"},{"label":"文件","value":"files"}],"multiSelect":true,"allowCustom":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	answers := []QuestionAnswer{{QuestionID: "entry", Values: []string{"desktop"}}, {QuestionID: "features", Values: []string{"browser", "files"}, Custom: "其他"}}
	if details := validateQuestionAnswers(card, answers); len(details) != 0 {
		t.Fatalf("details=%+v", details)
	}
	if details := validateQuestionAnswers(card, answers[:1]); len(details) == 0 {
		t.Fatal("missing answer accepted")
	}
	if details := validateQuestionAnswers(card, []QuestionAnswer{{QuestionID: "entry", Values: []string{"unknown"}}, answers[1]}); len(details) == 0 {
		t.Fatal("unknown option accepted")
	}
}
