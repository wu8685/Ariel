package protocol

import (
	"testing"
)

func TestExecutionSettingsContract(t *testing.T) {
	for _, body := range []string{
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"mac","method":"model.catalog.read","params":{}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"mac","method":"thread.settings.update","params":{"threadId":"t","expectedRevision":"r","operationId":"op","patch":{"speed":{"mode":"standard"}}}}`,
		`{"type":"event","v":1,"event":"thread.settings.update","deviceId":"mac","agentEpoch":"epoch","threadId":"t","subscriptionId":"s","streamId":"stream","seq":2,"settings":{"revision":"r","freshness":"fresh","writable":true,"values":{"model":"m","effort":"high","speed":{"mode":"tier","tierId":"priority"}}}}`,
	} {
		if err := Validate([]byte(body)); err != nil {
			t.Fatalf("new extension rejected: %v", err)
		}
	}
	for _, body := range []string{
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"mac","method":"thread.settings.update","params":{"threadId":"t","expectedRevision":"r","operationId":"op","patch":{"sandbox":"danger"}}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"mac","method":"thread.settings.update","params":{"threadId":"t","expectedRevision":"r","operationId":"op","patch":{"speed":{"mode":"standard","tierId":"priority"}}}}`,
	} {
		if Validate([]byte(body)) == nil {
			t.Fatal("accepted unsafe patch")
		}
	}
}
