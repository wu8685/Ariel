package protocol

import "encoding/json"

const TaskContract = "task-service/v1"
const TaskUserID = "ariel-owner"
const MaxTaskBytes = 256 << 10

func IsTaskMethod(method string) bool {
	switch method {
	case "task.list", "task.read", "task.create", "task.action", "task.feedback", "task.readAck":
		return true
	}
	return false
}

type TaskCommand struct {
	TaskStoreID       string   `json:"taskStoreId,omitempty"`
	CommandID         string   `json:"commandId"`
	TaskID            string   `json:"taskId"`
	AttemptID         string   `json:"attemptId"`
	ExecutionHostID   string   `json:"executionHostId"`
	BindingGeneration int64    `json:"bindingGeneration"`
	TaskVersion       int64    `json:"taskVersion"`
	Kind              string   `json:"kind"`
	TaskRevision      string   `json:"taskRevision,omitempty"`
	NativeThreadID    string   `json:"nativeThreadId,omitempty"`
	NativeTurnID      string   `json:"nativeTurnId,omitempty"`
	State             string   `json:"state,omitempty"`
	SourceSeq         int64    `json:"sourceSeq,omitempty"`
	FeedbackIDs       []string `json:"feedbackIds,omitempty"`
	ClientMessageID   string   `json:"clientMessageId"`
	Text              string   `json:"text"`
	LeaseToken        string   `json:"leaseToken"`
	CWD               string   `json:"cwd,omitempty"`
}
type TaskReceipt struct {
	CommandID       string `json:"commandId"`
	LeaseToken      string `json:"leaseToken"`
	Outcome         string `json:"outcome"`
	NativeThreadID  string `json:"nativeThreadId,omitempty"`
	NativeTurnID    string `json:"nativeTurnId,omitempty"`
	ClientMessageID string `json:"clientMessageId,omitempty"`
	Reason          string `json:"reason,omitempty"`
}
type TaskFact struct {
	Progress          string   `json:"progress,omitempty"`
	EventID           string   `json:"eventId"`
	CommandID         string   `json:"commandId"`
	TaskID            string   `json:"taskId"`
	AttemptID         string   `json:"attemptId"`
	ExecutionHostID   string   `json:"executionHostId"`
	BindingGeneration int64    `json:"bindingGeneration"`
	NativeThreadID    string   `json:"nativeThreadId"`
	NativeTurnID      string   `json:"nativeTurnId"`
	Status            string   `json:"status"`
	TaskAction        string   `json:"taskAction,omitempty"`
	TaskRevision      string   `json:"taskRevision,omitempty"`
	SourceSeq         int64    `json:"sourceSeq,omitempty"`
	FeedbackIDs       []string `json:"feedbackIds,omitempty"`
}
type TaskCapabilityFrame struct {
	Type            string `json:"type"`
	V               int    `json:"v"`
	TaskStoreID     string `json:"taskStoreId,omitempty"`
	ManagementReady bool   `json:"managementReady"`
	ExecutionReady  bool   `json:"executionReady"`
}
type TaskFrame struct {
	Type               string       `json:"type"`
	V                  int          `json:"v"`
	DispatchID         string       `json:"dispatchId"`
	ManagementDeviceID string       `json:"managementDeviceId"`
	TaskStoreID        string       `json:"taskStoreId"`
	ExecutionHostID    string       `json:"executionHostId"`
	Command            *TaskCommand `json:"command,omitempty"`
	Valid              *bool        `json:"valid,omitempty"`
	Receipt            *TaskReceipt `json:"receipt,omitempty"`
	Fact               *TaskFact    `json:"fact,omitempty"`
}

func (f TaskFrame) CloneAs(kind string) TaskFrame {
	f.Type = kind
	f.Command = nil
	f.Valid = nil
	f.Receipt = nil
	f.Fact = nil
	return f
}
func DecodeTaskFrame(raw []byte) (TaskFrame, error) {
	var f TaskFrame
	err := json.Unmarshal(raw, &f)
	return f, err
}
