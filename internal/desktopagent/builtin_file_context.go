package desktopagent

import (
	"crypto/rand"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/filecontext"
)

func configureFileContext(cfg *Config, rpc appserver.RPC) {
	if cfg.FileContext != nil || !(cfg.FileContextEnabled || cfg.ChillingEnabled) || cfg.TasksManagerOnly || rpc == nil {
		return
	}
	if cfg.AgentEpoch == "" {
		cfg.AgentEpoch = rand.Text()
	}
	cfg.FileContext = filecontext.NewWithOptions(appserver.FileContextSource{RPC: rpc}, filecontext.Options{WorkspaceBrowser: cfg.WorkspaceBrowserEnabled, DocumentAssets: cfg.DocumentAssetsEnabled, AgentEpoch: cfg.AgentEpoch})
}
