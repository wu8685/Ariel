package protocol

import (
	"strings"
	"testing"
)

func TestAsyncQuestionWireIsExplicitOptIn(t *testing.T) {
	for _, raw := range []string{
		`{"type":"request","v":1,"requestId":"request","deviceId":"device","method":"thread.cache.watch","params":{"threadId":"thread","asyncQuestionsVersion":1}}`,
		`{"type":"request","v":1,"requestId":"request","deviceId":"device","method":"thread.cache.touch","params":{"threadId":"thread","watching":true,"asyncQuestionsVersion":1}}`,
		`{"type":"request","v":1,"requestId":"request","deviceId":"device","method":"thread.questions.respond","params":{"threadId":"thread","agentEpoch":"epoch","turnId":"turn","questionId":"question","fingerprint":"fingerprint","operationId":"operation","answer":"Blue"}}`,
	} {
		raw = strings.ReplaceAll(raw, `"requestId":"request"`, `"requestId":"00000000-0000-4000-8000-000000000001"`)
		if err := Validate([]byte(raw)); err != nil {
			t.Fatalf("opt-in question contract rejected: %v", err)
		}
	}
}
