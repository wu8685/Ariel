package relay

import (
	"context"
	"encoding/json"
	"github.com/wu8685/Ariel/protocol"
	"testing"
)

func TestTaskManagementReadinessRecoversWithoutReplacingAgent(t *testing.T) {
	s, _ := taskFixtureRelay(t)
	credential := s.cfg.TaskDevices["manager"]
	p := &peer{role: "agent", deviceID: "manager", taskCredential: &credential, capabilities: map[string]bool{"autoLoad": false, "codexReady": false, "taskManagement": false}}
	s.agents["manager"] = p
	raw, _ := json.Marshal(protocol.TaskCapabilityFrame{Type: "task.capabilities", V: 1, TaskStoreID: "store", ManagementReady: true})
	if !s.handleMessage(context.Background(), p, raw) || !s.taskPublicAllowed(p, "task.list", json.RawMessage(`{"taskStoreId":"store"}`)) {
		t.Fatal("recovered backend did not update this authenticated Agent's task readiness")
	}
}
