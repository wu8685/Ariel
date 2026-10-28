package desktopipc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAsyncQuestionsProjectNativeMessageWithoutBlockingRequest(t *testing.T) {
	turns := []AsyncQuestionTurn{{ID: "turn", Status: "inProgress", Items: []json.RawMessage{json.RawMessage(`{"id":"message","type":"agentMessage","text":"Working independently","questions":[{"title":"Choose color","options":["Blue","Green"]},{"title":"Any note?"}]}`)}}}
	snapshot := ProjectAsyncQuestions("thread", turns)
	if len(snapshot.Questions) != 2 || snapshot.Questions[0].Title != "Choose color" || snapshot.Questions[0].Options[0] != "Blue" || snapshot.Questions[1].Status != "open" || snapshot.Writable {
		t.Fatalf("lost native questions or invented write support: %+v", snapshot)
	}
	copy := ProjectAsyncQuestions("thread", turns)
	if copy.Questions[0].ID != snapshot.Questions[0].ID || copy.Questions[0].Fingerprint == "" {
		t.Fatal("unstable question identity")
	}
	turns[0].Status = "completed"
	if got := ProjectAsyncQuestions("thread", turns); got.Questions[0].Status != "ended" || got.Questions[0].ID != snapshot.Questions[0].ID {
		t.Fatal("terminal question identity/status changed")
	}
}

func TestAsyncQuestionAnswersRequireAcceptedExactNativeEnvelope(t *testing.T) {
	question := json.RawMessage(`{"id":"message","type":"agentMessage","text":"Question","questions":[{"title":"Choose color","options":["Blue","Green"]}]}`)
	for _, status := range []string{"pending", "rejected", "accepted"} {
		body, _ := json.Marshal([]map[string]string{{"questionItemId": `["request_user_input_async","message",0]`, "question": "Choose color", "answer": "Blue"}})
		reply, _ := json.Marshal(map[string]any{"type": "steeringUserMessage", "targetTurnId": "turn", "status": status, "input": []any{map[string]string{"type": "text", "text": "<send_user_message_question_reply>\n" + string(body) + "\n</send_user_message_question_reply>"}}})
		got := ProjectAsyncQuestions("thread", []AsyncQuestionTurn{{ID: "turn", Status: "inProgress", Items: []json.RawMessage{question, reply}}})
		if (got.Questions[0].Status == "answered") != (status == "accepted") || (got.Questions[0].Answer == "Blue") != (status == "accepted") {
			t.Fatalf("optimistic answer or accepted answer lost: %s %+v", status, got)
		}
	}
}

func TestAsyncQuestionBoundsFailLocallyAndNeverInferPlainText(t *testing.T) {
	items := []json.RawMessage{json.RawMessage(`{"id":"plain","type":"agentMessage","text":"Please choose Blue or Green"}`)}
	large, _ := json.Marshal(map[string]any{"id": "large", "type": "agentMessage", "questions": []any{map[string]any{"title": strings.Repeat("x", 2049)}}})
	secret := json.RawMessage(`{"id":"secret","type":"agentMessage","questions":[{"title":"Password?","isSecret":true}]}`)
	items = append(items, large, secret)
	got := ProjectAsyncQuestions("thread", []AsyncQuestionTurn{{ID: "turn", Status: "inProgress", Items: items}})
	if len(got.Questions) != 0 || len(got.Warnings) != 2 {
		t.Fatalf("unsafe question projection: %+v", got)
	}
}

func TestAsyncQuestionPartialHistoryDoesNotInventUnansweredState(t *testing.T) {
	turn := AsyncQuestionTurn{ID: "turn", Status: "completed", Partial: true, Items: []json.RawMessage{json.RawMessage(`{"id":"message","type":"agentMessage","questions":[{"title":"Historical question"}]}`)}}
	got := ProjectAsyncQuestions("thread", []AsyncQuestionTurn{turn})
	if len(got.Questions) != 1 || got.Questions[0].Status != "unverified" {
		t.Fatal("partial item page invented unanswered state")
	}
}
