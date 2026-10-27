package protocol

import (
	"strings"
	"testing"
)

func TestDocumentAssetsContractIsNarrow(t *testing.T) {
	body := `{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"mac","method":"file.context.asset.read","params":{"source":"workspace_browser","threadId":"t","path":"docs/read.md","agentEpoch":"epoch","documentSha256":"` + strings.Repeat("a", 64) + `","imageReference":"../a.png"}}`
	if err := Validate([]byte(body)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(body, `"agentEpoch":"epoch",`, "", 1),
		strings.Replace(body, `"path":"docs/read.md"`, `"path":"/etc/passwd"`, 1),
		strings.Replace(body, `"source":"workspace_browser"`, `"source":"turn_change"`, 1),
		strings.Replace(body, `"imageReference":"../a.png"`, `"imageReference":"../a.png","root":"/"`, 1),
	} {
		if Validate([]byte(bad)) == nil {
			t.Fatalf("unsafe asset params validated: %s", bad)
		}
	}
}
