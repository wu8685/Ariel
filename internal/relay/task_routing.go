package relay

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/wu8685/Ariel/protocol"
)

const maxTaskRoutes = 64

type taskRoute struct {
	manager, executor *peer
	frame             protocol.TaskFrame
	timer             *time.Timer
}

func (s *Server) taskPublicAllowed(p *peer, method string, params json.RawMessage) bool {
	if !protocol.IsTaskMethod(method) {
		return true
	}
	if !s.cfg.TasksEnabled || p == nil || p.taskCredential == nil || !p.currentCapabilities()["taskManagement"] {
		return false
	}
	var scope struct {
		TaskStoreID     string `json:"taskStoreId"`
		ExecutionHostID string `json:"executionHostId"`
	}
	if len(params) > protocol.MaxTaskBytes || json.Unmarshal(params, &scope) != nil || scope.TaskStoreID != p.currentTaskStoreID() || !containsTaskValue(p.taskCredential.TaskStoreIDs, scope.TaskStoreID) {
		return false
	}
	return method != "task.create" || containsTaskValue(p.taskCredential.ExecutionTargets, scope.ExecutionHostID)
}

func (s *Server) handleTaskFrame(ctx context.Context, p *peer, body []byte) bool {
	if !s.cfg.TasksEnabled || p.role != "agent" || p.taskCredential == nil || len(body) > protocol.MaxTaskBytes {
		return false
	}
	f, err := protocol.DecodeTaskFrame(body)
	if err != nil {
		return false
	}
	if f.Type == "task.dispatch" || f.Type == "task.track" {
		return s.dispatchTask(ctx, p, f)
	}
	s.mu.Lock()
	route := s.taskRoutes[f.DispatchID]
	if route == nil {
		s.mu.Unlock()
		return true
	}
	if f.ManagementDeviceID != route.frame.ManagementDeviceID || f.ExecutionHostID != route.frame.ExecutionHostID || f.TaskStoreID != route.frame.TaskStoreID {
		s.mu.Unlock()
		return false
	}
	var target *peer
	switch f.Type {
	case "task.validate":
		if p != route.executor {
			s.mu.Unlock()
			return false
		}
		target = route.manager
	case "task.validated":
		if p != route.manager || f.Valid == nil || (*f.Valid && (f.Command == nil || !reflect.DeepEqual(*f.Command, *route.frame.Command))) {
			s.mu.Unlock()
			return false
		}
		target = route.executor
	case "task.receipt":
		if p != route.executor || f.Receipt == nil || f.Receipt.CommandID != route.frame.Command.CommandID || f.Receipt.LeaseToken != route.frame.Command.LeaseToken {
			s.mu.Unlock()
			return false
		}
		target = route.manager
		route.timer.Stop()
		if f.Receipt.Outcome != "accepted" {
			delete(s.taskRoutes, f.DispatchID)
		}
	case "task.fact":
		c := route.frame.Command
		if p != route.executor || f.Fact == nil || f.Fact.CommandID != c.CommandID || f.Fact.TaskID != c.TaskID || f.Fact.AttemptID != c.AttemptID || f.Fact.ExecutionHostID != c.ExecutionHostID || f.Fact.BindingGeneration != c.BindingGeneration {
			s.mu.Unlock()
			return false
		}
		target = route.manager
		if f.Fact.Status == "completed" || f.Fact.Status == "failed" {
			route.timer.Stop()
			delete(s.taskRoutes, f.DispatchID)
		}
	default:
		s.mu.Unlock()
		return false
	}
	if s.agents[target.deviceID] != target || s.agents[p.deviceID] != p {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	_ = target.send(ctx, f)
	return true
}

func (s *Server) dispatchTask(ctx context.Context, p *peer, f protocol.TaskFrame) bool {
	if p.deviceID != f.ManagementDeviceID || !p.currentCapabilities()["taskManagement"] || p.currentTaskStoreID() != f.TaskStoreID || f.Command == nil || f.Command.ExecutionHostID != f.ExecutionHostID || (f.Command.TaskStoreID != "" && f.Command.TaskStoreID != f.TaskStoreID) {
		return false
	}
	// A previously persisted claim can outlive a deployment's target allowlist.
	// Reject that command, not the authenticated manager's ordinary connection.
	if !containsTaskValue(p.taskCredential.ExecutionTargets, f.ExecutionHostID) {
		reply := f.CloneAs("task.receipt")
		reply.Receipt = &protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "failed", Reason: "TASK_UNAUTHORIZED"}
		_ = p.send(ctx, reply)
		return true
	}
	s.mu.Lock()
	if s.taskRoutes == nil {
		s.taskRoutes = map[string]*taskRoute{}
	}
	if s.agents[p.deviceID] != p || s.taskRoutes[f.DispatchID] != nil {
		s.mu.Unlock()
		return false
	}
	target := s.agents[f.ExecutionHostID]
	ready := target != nil && target.currentCapabilities()["taskExecution"]
	if f.Type == "task.track" && target != nil {
		ready = target.currentCapabilities()["taskTracking"] || target.currentCapabilities()["taskExecution"]
	}
	if target == nil || target.taskCredential == nil || !target.taskCredential.Execute || !ready {
		reason := "TASK_EXECUTION_UNAVAILABLE"
		if target == nil {
			reason = "DEVICE_OFFLINE"
		}
		s.mu.Unlock()
		reply := f.CloneAs("task.receipt")
		reply.Receipt = &protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "busy", Reason: reason}
		_ = p.send(ctx, reply)
		return true
	}
	if len(s.taskRoutes) >= maxTaskRoutes {
		s.mu.Unlock()
		reply := f.CloneAs("task.receipt")
		reply.Receipt = &protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "busy", Reason: "OVERLOADED"}
		_ = p.send(ctx, reply)
		return true
	}
	entry := &taskRoute{manager: p, executor: target, frame: f}
	s.taskRoutes[f.DispatchID] = entry
	entry.timer = time.AfterFunc(s.cfg.MutationTimeout, func() {
		s.mu.Lock()
		if s.taskRoutes[f.DispatchID] != entry {
			s.mu.Unlock()
			return
		}
		delete(s.taskRoutes, f.DispatchID)
		s.mu.Unlock()
		reply := f.CloneAs("task.receipt")
		reply.Receipt = &protocol.TaskReceipt{CommandID: f.Command.CommandID, LeaseToken: f.Command.LeaseToken, Outcome: "unknown", Reason: "OUTCOME_UNKNOWN"}
		_ = p.send(context.Background(), reply)
	})
	s.mu.Unlock()
	if target.send(ctx, f) != nil {
		s.removeTaskPeer(target)
	}
	return true
}

func (s *Server) removeTaskPeer(p *peer) {
	s.mu.Lock()
	var failed []*taskRoute
	for id, r := range s.taskRoutes {
		if r.manager == p || r.executor == p {
			delete(s.taskRoutes, id)
			r.timer.Stop()
			if r.executor == p && r.manager != p {
				failed = append(failed, r)
			}
		}
	}
	s.mu.Unlock()
	for _, r := range failed {
		f := r.frame.CloneAs("task.receipt")
		f.Receipt = &protocol.TaskReceipt{CommandID: r.frame.Command.CommandID, LeaseToken: r.frame.Command.LeaseToken, Outcome: "unknown", Reason: "OUTCOME_UNKNOWN"}
		_ = r.manager.send(context.Background(), f)
	}
}

func isTaskFrameType(kind string) bool { return strings.HasPrefix(kind, "task.") }
