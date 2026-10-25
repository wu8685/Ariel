package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type officialGateRPC struct {
	account, config, thread string
	fail                    string
	calls                   []string
}

func (f *officialGateRPC) Call(_ context.Context, method string, params any, out any) error {
	f.calls = append(f.calls, method)
	if method == f.fail {
		return errors.New("secret remote error must not escape")
	}
	var body string
	switch method {
	case "account/read":
		p, _ := json.Marshal(params)
		if string(p) != `{"refreshToken":false}` {
			return errors.New("must not refresh account")
		}
		body = f.account
	case "config/read":
		body = f.config
	case "thread/read":
		body = f.thread
	default:
		return errors.New("unexpected mutation")
	}
	return json.Unmarshal([]byte(body), out)
}

func TestOfficialTaskGateRequiresEffectiveOfficialConfigurationAndExactThread(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	tests := []struct {
		name, account, config, thread, id string
		allow                             bool
	}{
		{"official before create", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai"}}`, ``, ``, true},
		{"documented default provider", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":null}}`, ``, ``, true},
		{"official chatgpt base", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":null,"chatgpt_base_url":"https://chatgpt.com/backend-api/"}}`, ``, ``, true},
		{"official API base", `{"account":{"type":"apiKey"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai","openai_base_url":"https://api.openai.com/v1"}}`, ``, ``, true},
		{"deceptive official domain", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai","chatgpt_base_url":"https://chatgpt.com.third-party.invalid/backend-api"}}`, ``, ``, false},
		{"custom chatgpt endpoint", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai","chatgpt_base_url":"https://third-party.invalid"}}`, ``, ``, false},
		{"official existing", `{"account":{"type":"apiKey"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai"}}`, `{"thread":{"id":"fixture-thread","modelProvider":"openai"}}`, `fixture-thread`, true},
		{"missing account", `{"account":null,"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai"}}`, ``, ``, false},
		{"third-party provider", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"other"}}`, ``, ``, false},
		{"unknown config", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{}}`, ``, ``, false},
		{"authentication bypass", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":false}`, `{"config":{"model_provider":"openai"}}`, ``, ``, false},
		{"provider override", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai","model_providers":{"openai":{"base_url":"https://third-party.invalid/v1"}}}}`, ``, ``, false},
		{"native provider mismatch", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai"}}`, `{"thread":{"id":"fixture-thread","modelProvider":"other"}}`, `fixture-thread`, false},
		{"wrong native thread", `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, `{"config":{"model_provider":"openai"}}`, `{"thread":{"id":"different-thread","modelProvider":"openai"}}`, `fixture-thread`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &officialGateRPC{account: tt.account, config: tt.config, thread: tt.thread}
			err := NewOfficialTaskCheck(f)(context.Background(), tt.id, "")
			if (err == nil) != tt.allow {
				t.Fatalf("allow=%v error=%v calls=%v", tt.allow, err, f.calls)
			}
			for _, method := range f.calls {
				if method != "account/read" && method != "config/read" && method != "thread/read" {
					t.Fatalf("mutation %s", method)
				}
			}
		})
	}
}

func TestOfficialTaskGateFailsClosedWithoutLeakingConfigOrErrors(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	f := &officialGateRPC{fail: "account/read"}
	err := NewOfficialTaskCheck(f)(context.Background(), "", "")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error %v", err)
	}
	if NewOfficialTaskCheck(nil)(context.Background(), "", "") == nil {
		t.Fatal("nil account source accepted")
	}
	t.Setenv("OPENAI_BASE_URL", "https://third-party.invalid/v1")
	f = &officialGateRPC{account: `{"account":{"type":"chatgpt"},"requiresOpenaiAuth":true}`, config: `{"config":{"model_provider":"openai"}}`}
	if NewOfficialTaskCheck(f)(context.Background(), "", "") == nil {
		t.Fatal("environment endpoint override accepted")
	}
}

func TestOfficialTaskGateInstalledReadOnly(t *testing.T) {
	binary := os.Getenv("ARIEL_OFFICIAL_GATE_BINARY")
	if binary == "" {
		t.Skip("set ARIEL_OFFICIAL_GATE_BINARY for isolated read-only installed-version gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cwd := t.TempDir()
	process, err := appserver.Start(ctx, binary, cwd)
	if err != nil {
		t.Fatal("isolated read-only App Server failed to initialize")
	}
	defer process.Close()
	if err := NewOfficialTaskCheck(process)(ctx, "", cwd); err != nil {
		var account struct {
			Account *struct {
				Type string `json:"type"`
			} `json:"account"`
			Required bool `json:"requiresOpenaiAuth"`
		}
		var config struct {
			Config map[string]json.RawMessage `json:"config"`
		}
		accountErr := process.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &account)
		configErr := process.Call(ctx, "config/read", map[string]any{"includeLayers": false, "cwd": cwd}, &config)
		var provider string
		_ = json.Unmarshal(config.Config["model_provider"], &provider)
		_, providerPresent := config.Config["model_provider"]
		var providerDefinitions map[string]json.RawMessage
		_ = json.Unmarshal(config.Config["model_providers"], &providerDefinitions)
		_, openaiDefinition := providerDefinitions["openai"]
		accountKnown := account.Account != nil && (account.Account.Type == "chatgpt" || account.Account.Type == "apiKey")
		t.Logf("redacted decision evidence: accountKnown=%v providerFieldPresent=%v providerNull=%v selectedProviderDefinition=%v openaiBaseConfigured=%v chatgptBaseConfigured=%v", accountKnown, providerPresent, string(config.Config["model_provider"]) == "null", openaiDefinition, len(config.Config["openai_base_url"]) > 0 && string(config.Config["openai_base_url"]) != "null", len(config.Config["chatgpt_base_url"]) > 0 && string(config.Config["chatgpt_base_url"]) != "null")
		t.Logf("redacted gate evidence: accountRead=%v accountPresent=%v requiresOpenAI=%v configRead=%v providerOpenAI=%v providerUnset=%v providerOverridesPresent=%v envOverride=%v", accountErr == nil, account.Account != nil, account.Required, configErr == nil, provider == "openai", provider == "", len(config.Config["model_providers"]) > 0, os.Getenv("OPENAI_BASE_URL") != "")
		t.Fatalf("installed configuration is not verified: %v", err)
	}
}
