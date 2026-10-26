package protocol

import (
	"strings"
	"testing"
)

func TestTurnEditContract(t *testing.T) {
	body := `{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"mac","method":"turn.edit","params":{"threadId":"t","agentEpoch":"epoch","expectedTurnId":"old","expectedItemId":"user","fingerprint":"` + strings.Repeat("a", 64) + `","operationId":"op","text":"new"}}`
	if e := Validate([]byte(body)); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(body, `"text":"new"`, `"text":""`, 1), strings.Replace(body, `"text":"new"`, `"text":"new","sandbox":"danger-full-access"`, 1), strings.Replace(body, `"agentEpoch":"epoch",`, "", 1)} {
		if Validate([]byte(bad)) == nil {
			t.Fatal("unsafe edit validated")
		}
	}
}
