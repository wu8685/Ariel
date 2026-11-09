package desktopagent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeTurn61AutomaticApprovalReviews(t *testing.T) {
	t.Run("single approved file review", func(t *testing.T) {
		turn, err := normalizeTurn("turn", "completed", []json.RawMessage{
			json.RawMessage(`{"id":"review-1","type":"automaticApprovalReview","status":"approved","action":{"type":"applyPatch","files":["/private/fixture.txt"]},"rationale":"must-not-leak"}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		items := turn["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("items=%v", items)
		}
		got := items[0].(map[string]any)
		if got["text"] != "已自动审批 · 文件\n文件 · 已批准" || strings.Contains(got["text"].(string), "private") || len(got) != 3 {
			t.Fatalf("unsafe automatic approval projection: %#v", got)
		}
	})

	t.Run("consecutive reviews are one quiet item", func(t *testing.T) {
		turn, err := normalizeTurn("turn", "completed", []json.RawMessage{
			json.RawMessage(`{"id":"review-1","type":"automaticApprovalReview","status":"approved","action":{"type":"command","command":"secret one"}}`),
			json.RawMessage(`{"id":"review-2","type":"automaticApprovalReview","status":"approved","action":{"type":"applyPatch","files":["secret.txt"]}}`),
			json.RawMessage(`{"id":"review-3","type":"automaticApprovalReview","status":"approved","action":{"type":"networkAccess","target":"private.example"}}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		items := turn["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("reviews not merged: %v", items)
		}
		text := items[0].(map[string]any)["text"].(string)
		if text != "已自动审批 3 项\n命令 · 已批准\n文件 · 已批准\n网络 · 已批准" || strings.Contains(text, "secret") || strings.Contains(text, "private.example") {
			t.Fatalf("review summary=%q", text)
		}
	})

	t.Run("denied review is never called approved", func(t *testing.T) {
		turn, err := normalizeTurn("turn", "completed", []json.RawMessage{
			json.RawMessage(`{"id":"review-1","type":"automaticApprovalReview","status":"denied","action":{"type":"execve","program":"private","argv":["value"]}}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		text := turn["items"].([]any)[0].(map[string]any)["text"].(string)
		if text != "自动审批检查 · 命令\n命令 · 未通过" || strings.Contains(text, "private") || strings.Contains(text, "已自动审批") {
			t.Fatalf("denial=%q", text)
		}
	})
}

func TestNormalizeTurn61SteeredAndUnknownItems(t *testing.T) {
	turn, err := normalizeTurn("turn", "completed", []json.RawMessage{
		json.RawMessage(`{"id":"steered-1","type":"steered","private":"must-not-leak"}`),
		json.RawMessage(`{"id":"compact-1","type":"contextCompaction","private":"must-not-leak"}`),
		json.RawMessage(`{"id":"future-1","type":"futureTool<script>","payload":"must-not-leak"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	items := turn["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items=%v", items)
	}
	want := []string{"已插入引导", "已压缩上下文", "暂不支持显示的内容\n类型：futureToolscript"}
	for i, expected := range want {
		got := items[i].(map[string]any)
		if got["text"] != expected || strings.Contains(got["text"].(string), "must-not-leak") || strings.Contains(got["text"].(string), "[futureTool") || len(got) != 3 {
			t.Fatalf("item %d=%#v want=%q", i, got, expected)
		}
	}
}

func TestNormalizeTurn61QuestionReplyEnvelope(t *testing.T) {
	valid := `<send_user_message_question_reply>
[{"questionItemId":["request_user_input_async","call-fixture",0],"question":"Which fixture?","answer":"Blue"},{"questionItemId":"question-2","question":"Why?","answer":"Safe reason"}]
</send_user_message_question_reply>`
	invalid := `<send_user_message_question_reply>{not-json}</send_user_message_question_reply>`
	rawValid, _ := json.Marshal(map[string]any{"id": "answer-1", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": valid}}})
	rawInvalid, _ := json.Marshal(map[string]any{"id": "answer-2", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": invalid}}})
	turn, err := normalizeTurn("turn", "completed", []json.RawMessage{rawValid, rawInvalid})
	if err != nil {
		t.Fatal(err)
	}
	items := turn["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items=%v", items)
	}
	validText := items[0].(map[string]any)["text"].(string)
	if validText != "> 问题：Which fixture?\n\n你的回答：Blue\n\n> 问题：Why?\n\n你的回答：Safe reason" {
		t.Fatalf("valid reply=%q", validText)
	}
	for _, forbidden := range []string{"send_user_message_question_reply", "questionItemId", "request_user_input_async", "call-fixture"} {
		if strings.Contains(validText, forbidden) {
			t.Fatalf("internal envelope leaked: %q", validText)
		}
	}
	invalidText := items[1].(map[string]any)["text"].(string)
	if invalidText != "已提交补充回答，但当前格式暂不支持显示。" || strings.Contains(invalidText, "not-json") || items[1].(map[string]any)["role"] != "user" {
		t.Fatalf("invalid reply=%#v", items[1])
	}
}
