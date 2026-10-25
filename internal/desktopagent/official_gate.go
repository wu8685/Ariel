package desktopagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type officialTaskRPC interface {
	Call(context.Context, string, any, any) error
}

var errOfficialTaskUnverified = errors.New("OFFICIAL_PROVIDER_UNVERIFIED")

// NewOfficialTaskCheck inspects account and effective project configuration
// without exposing either to the Relay or issuing an inference/mutation. This
// intentionally rejects unknown configuration and customized OpenAI providers;
// a model name or bundled catalog does not prove an official endpoint.
func NewOfficialTaskCheck(rpc officialTaskRPC) func(context.Context, string, string) error {
	return func(ctx context.Context, threadID, cwd string) error {
		if rpc == nil || (cwd != "" && !filepath.IsAbs(cwd)) || os.Getenv("OPENAI_BASE_URL") != "" {
			return errOfficialTaskUnverified
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var account struct {
			Account *struct {
				Type string `json:"type"`
			} `json:"account"`
			RequiresOpenAIAuth bool `json:"requiresOpenaiAuth"`
		}
		if err := rpc.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &account); err != nil || account.Account == nil || !account.RequiresOpenAIAuth || (account.Account.Type != "chatgpt" && account.Account.Type != "apiKey") {
			return errOfficialTaskUnverified
		}
		var config struct {
			Config map[string]json.RawMessage `json:"config"`
		}
		params := map[string]any{"includeLayers": false}
		if cwd != "" {
			params["cwd"] = cwd
		}
		if err := rpc.Call(ctx, "config/read", params, &config); err != nil || config.Config == nil {
			return errOfficialTaskUnverified
		}
		var provider string
		rawProvider, present := config.Config["model_provider"]
		// The public nullable config shape does not materialize defaults. The
		// documented built-in default is openai; missing/invalid shapes are not
		// treated as that default.
		if !present {
			return errOfficialTaskUnverified
		}
		if string(rawProvider) == "null" {
			provider = "openai"
		} else if json.Unmarshal(rawProvider, &provider) != nil {
			return errOfficialTaskUnverified
		}
		if provider != "openai" {
			return errOfficialTaskUnverified
		}
		if raw, ok := config.Config["model_providers"]; ok && string(raw) != "null" {
			var providers map[string]json.RawMessage
			if json.Unmarshal(raw, &providers) != nil {
				return errOfficialTaskUnverified
			}
			if _, overridden := providers["openai"]; overridden {
				return errOfficialTaskUnverified
			}
		}
		for _, key := range []string{"openai_base_url", "chatgpt_base_url"} {
			if !officialTaskBaseURL(config.Config[key], key) {
				return errOfficialTaskUnverified
			}
		}
		if threadID != "" {
			var read struct {
				Thread struct {
					ID       string `json:"id"`
					Provider string `json:"modelProvider"`
					CWD      string `json:"cwd"`
				} `json:"thread"`
			}
			if err := rpc.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, &read); err != nil || read.Thread.ID != threadID || read.Thread.Provider != "openai" {
				return errOfficialTaskUnverified
			}
			if cwd != "" && (read.Thread.CWD == "" || filepath.Clean(read.Thread.CWD) != filepath.Clean(cwd)) {
				return errOfficialTaskUnverified
			}
		}
		return nil
	}
}

func officialTaskBaseURL(raw json.RawMessage, key string) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || u.Port() != "" {
		return false
	}
	if key == "openai_base_url" {
		return u.Host == "api.openai.com" && (u.Path == "/v1" || u.Path == "/v1/")
	}
	if u.Host != "chatgpt.com" {
		return false
	}
	switch u.Path {
	case "", "/", "/backend-api", "/backend-api/", "/backend-api/codex", "/backend-api/codex/":
		return true
	}
	return false
}
