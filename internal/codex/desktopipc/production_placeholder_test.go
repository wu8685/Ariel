package desktopipc

import (
	"encoding/json"
	"testing"
)

func TestTransientCanonicalPlaceholderIsStrict(t *testing.T) {
	cases := []struct {
		name, state string
		want        bool
	}{
		{"single empty active", `{"cwd":"/fixture","threadRuntimeStatus":{"type":"active"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"inProgress","items":[]}}}}}`, true},
		{"idle ghost", `{"threadRuntimeStatus":{"type":"idle"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"inProgress","items":[]}}}}}`, false},
		{"pending request", `{"threadRuntimeStatus":{"type":"active"},"requests":[{}],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"inProgress","items":[]}}}}}`, false},
		{"ghost has item", `{"threadRuntimeStatus":{"type":"active"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"inProgress","items":[{}]}}}}}`, false},
		{"ghost completed", `{"threadRuntimeStatus":{"type":"active"},"requests":[],"turnHistory":{"kind":"canonical","history":{"islands":[{"entries":[{"value":"ghost"}]}],"entitiesByKey":{"ghost":{"turnId":"","status":"completed","items":[]}}}}}`, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := TransientCanonicalPlaceholder(json.RawMessage(tt.state)); got != tt.want {
				t.Fatalf("placeholder=%t, want %t", got, tt.want)
			}
		})
	}
}
