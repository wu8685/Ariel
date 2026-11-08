package protocol

import "testing"

func TestPuckProtocolExtensionsAreOptionalAndBounded(t *testing.T) {
	valid := []string{
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"off"}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"ready"}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"unavailable","reason":"PUCK_REQUIRES_PASSKEY_HTTPS"}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"degraded","reason":"PUCK_BRAIN_UNAVAILABLE"}}`,
		`{"type":"hello","v":1,"role":"agent","token":"secret","deviceId":"device-a","deviceName":"Mac","agentEpoch":"epoch-a","adapterVersion":"desktop-ipc-0.160.0","capabilities":{"autoLoad":true,"codexReady":false,"puckBrain":true,"puckBrainReady":true,"executionPolicy":false}}`,
		`{"type":"event","v":1,"event":"device.status","deviceId":"device-a","agentOnline":true,"codexReady":false,"capabilities":{"autoLoad":true,"codexReady":false,"puckBrain":true,"puckBrainReady":true,"executionPolicy":false}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.list","params":{}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.send","params":{"assistantId":"assistant-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"你好"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.send","params":{"assistantId":"assistant-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"继续","replyTo":"message-a"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.get","params":{"assistantId":"assistant-a","messageId":"message-a"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.send","params":{"assistantId":"assistant-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"","images":["data:image/png;base64,AAAA"]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.update","params":{"assistantId":"assistant-a","avatar":{"shape":"pumpkin","color":"#ff2f92"}}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.create","params":{"name":"Helper","brainDeviceId":"device-a","permissionMode":"full"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000002","deviceId":"relay","method":"puck.authorization.decide","params":{"assistantId":"assistant-a","authorizationId":"auth-a","decision":"approve_always"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000003","deviceId":"relay","method":"puck.authorization.rule.list","params":{"assistantId":"assistant-a"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000004","deviceId":"relay","method":"puck.authorization.rule.revoke","params":{"assistantId":"assistant-a","ruleId":"rule-a"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","origin":{"kind":"puck","assistantId":"assistant-a","trigger":"tool","permissionMode":"full"},"method":"turn.start","params":{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"go"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","origin":{"kind":"puck","assistantId":"assistant-a","trigger":"user_decision","permissionMode":"ask"},"method":"turn.start","params":{"threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"approved"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.read","params":{"assistantId":"assistant-a"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.react","params":{"assistantId":"assistant-a","messageId":"message-a","emoji":"👍"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"puck.brain.turn.start","params":{"assistantId":"assistant-a","threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"信封内容"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"puck.brain.turn.start","params":{"assistantId":"assistant-a","threadId":"thread-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"信封内容","images":["data:image/jpeg;base64,AAAA"]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"puck.brain.toolResult","params":{"threadId":"thread-a","callId":"call-a","success":true,"contentItems":[{"type":"inputText","text":"ok"}]}}`,
		`{"type":"event","v":1,"event":"puck.brain.toolCall","deviceId":"device-a","threadId":"thread-a","callId":"call-a","tool":"start_task","arguments":{"clientTaskId":"task-a"}}`,
		`{"type":"event","v":1,"event":"puck.brain.event","deviceId":"device-a","threadId":"thread-a","kind":"message","payload":{"text":"收到"}}`,
		`{"type":"event","v":1,"event":"puck.event","assistantId":"assistant-a","seq":1,"kind":"message","payload":{"text":"收到"}}`,
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"rejected","error":{"code":"FEATURE_DISABLED","message":"Puck unavailable","reason":"PUCK_REQUIRES_PASSKEY_HTTPS"}}`,
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"rejected","error":{"code":"EXECUTION_POLICY_UNENFORCEABLE","message":"policy unavailable"}}`,
		`{"type":"response","v":1,"requestId":"00000000-0000-4000-8000-000000000001","outcome":"rejected","error":{"code":"PUCK_SELF_TARGET","message":"self target"}}`,
	}
	for _, raw := range valid {
		if err := Validate([]byte(raw)); err != nil {
			t.Errorf("valid Puck message rejected: %s: %v", raw, err)
		}
	}
}

func TestPuckProtocolRejectsIdentityAndShapeConfusion(t *testing.T) {
	invalid := []string{
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"ready","reason":"must-not-exist"}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"unavailable"}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"degraded"}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"unknown"}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","puck":{"state":"ready","extra":true}}`,
		`{"type":"hello.ok","v":1,"connectionId":"conn-a","relayEpoch":"relay-a","unknown":true}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.send","params":{"assistantId":"assistant-a","text":"missing id"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.send","params":{"assistantId":"assistant-a","clientMessageId":"00000000-0000-4000-8000-000000000002","text":"","images":[]}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"device-a","method":"puck.brain.turn.start","params":{"assistantId":"assistant-a","threadId":"thread-a","clientMessageId":"bad","text":"x"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.update","params":{"assistantId":"assistant-a","avatar":{"shape":"octagon","color":"#ff2f92"}}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.update","params":{"assistantId":"assistant-a","avatar":{"shape":"star","color":"red"}}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.update","params":{"assistantId":"assistant-a","permissionMode":"root"}}`,
		`{"type":"request","v":1,"requestId":"00000000-0000-4000-8000-000000000001","deviceId":"relay","method":"puck.message.react","params":{"assistantId":"assistant-a","messageId":"message-a","emoji":"🔥"}}`,
		`{"type":"event","v":1,"event":"puck.brain.toolCall","deviceId":"device-a","assistantId":"spoofed","threadId":"thread-a","callId":"call-a","tool":"start_task","arguments":{}}`,
		`{"type":"event","v":1,"event":"puck.brain.toolCall","deviceId":"device-a","threadId":"thread-a","callId":"call-a","tool":"start_task","arguments":"not-object"}`,
	}
	for _, raw := range invalid {
		if err := Validate([]byte(raw)); err == nil {
			t.Errorf("invalid Puck message accepted: %s", raw)
		}
	}
}
