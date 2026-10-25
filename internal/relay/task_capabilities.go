package relay

import (
	"context"
	"encoding/json"

	"github.com/wu8685/Ariel/protocol"
)

type taskPeerState struct {
	storeID                         string
	managementReady, executionReady bool
}

func (p *peer) currentTaskStoreID() string {
	if state := p.taskState.Load(); state != nil {
		return state.storeID
	}
	return p.taskStoreID
}
func (p *peer) currentCapabilities() map[string]bool {
	capabilities := make(map[string]bool, len(p.capabilities))
	for k, v := range p.capabilities {
		capabilities[k] = v
	}
	if state := p.taskState.Load(); state != nil {
		capabilities["taskManagement"] = state.managementReady
		capabilities["taskExecution"] = state.executionReady
	}
	return capabilities
}
func (s *Server) handleTaskCapabilities(ctx context.Context, p *peer, body []byte) bool {
	if !s.cfg.TasksEnabled || p.taskCredential == nil {
		return false
	}
	var update protocol.TaskCapabilityFrame
	if json.Unmarshal(body, &update) != nil {
		return false
	}
	if update.TaskStoreID != "" && !containsTaskValue(p.taskCredential.TaskStoreIDs, update.TaskStoreID) {
		return false
	}
	if update.ManagementReady && update.TaskStoreID == "" {
		return false
	}
	authorizedExecution := p.taskCredential.Execute && (p.capabilities["taskExecutionAuthorized"] || p.capabilities["taskExecution"])
	p.taskState.Store(&taskPeerState{storeID: update.TaskStoreID, managementReady: update.ManagementReady, executionReady: update.ExecutionReady && authorizedExecution})
	s.broadcastDevice(p.deviceID, p)
	return true
}
