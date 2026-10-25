package protocol

import "testing"

func TestTaskRPCAndBackgroundFramesHaveBoundedPublicContracts(t *testing.T) {
	valid := []string{
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"rejected","error":{"code":"TASK_VERSION_CONFLICT","message":"TASK_VERSION_CONFLICT"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"manager","method":"task.list","params":{"taskStoreId":"store","limit":20}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000002","deviceId":"manager","method":"task.create","params":{"taskStoreId":"store","clientSubmissionId":"submission","title":"Review","goal":"Review the fixture","executionHostId":"executor","cwd":"/fixture"}}`,
		`{"type":"task.dispatch","v":1,"dispatchId":"dispatch-1","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","command":{"commandId":"command-1","taskId":"task-1","attemptId":"attempt-1","executionHostId":"executor","bindingGeneration":1,"taskVersion":1,"kind":"start","clientMessageId":"message-1","text":"Review the fixture","leaseToken":"lease-1"}}`,
		`{"type":"task.dispatch","v":1,"dispatchId":"dispatch-claim","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","command":{"commandId":"command-claim","taskId":"task-1","attemptId":"attempt-claim","executionHostId":"executor","bindingGeneration":1,"taskVersion":1,"kind":"claim","clientMessageId":"message-claim","text":"Return the claim intent","leaseToken":"lease-claim","taskRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","cwd":"/fixture"}}`,
		`{"type":"task.fact","v":1,"dispatchId":"dispatch-claim","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","fact":{"eventId":"event-claim","commandId":"command-claim","taskId":"task-1","attemptId":"attempt-claim","executionHostId":"executor","bindingGeneration":1,"nativeThreadId":"thread-1","nativeTurnId":"turn-1","status":"completed","taskAction":"claim","taskRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
		`{"type":"task.validate","v":1,"dispatchId":"dispatch-1","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor"}`,
		`{"type":"task.track","v":1,"dispatchId":"dispatch-1","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","command":{"commandId":"command-1","taskId":"task-1","attemptId":"attempt-1","executionHostId":"executor","bindingGeneration":1,"taskVersion":1,"kind":"start","clientMessageId":"message-1","text":"Review the fixture","leaseToken":"lease-1","nativeThreadId":"thread-1","nativeTurnId":"turn-1","state":"accepted","feedbackIds":["feedback-1"]}}`,
		`{"type":"task.receipt","v":1,"dispatchId":"dispatch-1","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","receipt":{"commandId":"command-1","leaseToken":"lease-1","outcome":"unknown","reason":"OUTCOME_UNKNOWN"}}`,
	}
	for _, body := range valid {
		if err := Validate([]byte(body)); err != nil {
			t.Errorf("task contract rejected: %s: %v", body, err)
		}
	}
	invalid := []string{
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"manager","method":"task.list","params":{"taskStoreId":"store","userId":"someone-else"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"manager","method":"task.proxy","params":{"taskStoreId":"store","url":"http://localhost"}}`,
		`{"type":"task.receipt","v":1,"dispatchId":"dispatch-1","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","receipt":{"commandId":"command-1","leaseToken":"lease-1","outcome":"done"}}`,
		`{"type":"task.dispatch","v":1,"dispatchId":"dispatch-claim","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","command":{"commandId":"command-claim","taskId":"task-1","attemptId":"attempt-claim","executionHostId":"executor","bindingGeneration":1,"taskVersion":1,"kind":"claim","clientMessageId":"message-claim","text":"missing revision","leaseToken":"lease-claim","cwd":"/fixture"}}`,
		`{"type":"task.fact","v":1,"dispatchId":"dispatch-claim","managementDeviceId":"manager","taskStoreId":"store","executionHostId":"executor","fact":{"eventId":"event-claim","commandId":"command-claim","taskId":"task-1","attemptId":"attempt-claim","executionHostId":"executor","bindingGeneration":1,"nativeThreadId":"thread-1","nativeTurnId":"turn-1","status":"completed","taskAction":"claim"}}`,
	}
	for _, body := range invalid {
		if err := Validate([]byte(body)); err == nil {
			t.Errorf("unsafe task contract accepted: %s", body)
		}
	}
}
