import { describe, expect, it } from "vitest";
import { applyThreadEvent, belongsToSubscription, preserveDraftAfterSend, recoveryTarget, keepOfflineDevice, type ThreadView } from "./state";

const thread = { threadId: "t", title: "Same", cwd: "/a", updatedAt: "2026-10-04T00:00:00Z", runtime: "inProgress" as const, turns: [{ turnId: "turn", status: "inProgress" as const, items: [{ itemId: "assistant", role: "assistant" as const, text: "A" }] }], pendingInteractions: [] };
const snapshot = { type: "event" as const, v: 1 as const, event: "thread.snapshot" as const, deviceId: "d", threadId: "t", subscriptionId: "sub", streamId: "stream", seq: 1 as const, thread };

describe("thread stream state", () => {
  it("accepts snapshot then replaces item by ID without duplicating text", () => {
    const first = applyThreadEvent(null, snapshot);
    expect(first?.seq).toBe(1);
    const update = { ...snapshot, event: "thread.update" as const, baseSeq: 1, seq: 2, thread: { ...thread, turns: [{ ...thread.turns[0], items: [{ itemId: "assistant", role: "assistant" as const, text: "AB" }] }] } };
    const second = applyThreadEvent(first, update);
    expect(second?.thread.turns[0].items).toHaveLength(1);
    expect(second?.thread.turns[0].items[0].text).toBe("AB");
  });

  it("rejects gaps and unknown streams to force resubscribe", () => {
    const first = applyThreadEvent(null, snapshot) as ThreadView;
    expect(applyThreadEvent(first, { ...snapshot, event: "thread.update", baseSeq: 1, seq: 3 })).toBeNull();
    expect(applyThreadEvent(first, { ...snapshot, event: "thread.update", streamId: "other", baseSeq: 1, seq: 2 })).toBeNull();
  });

  it("ignores late events from an old subscription after resubscribing the same thread", () => {
    expect(belongsToSubscription(snapshot, "d", "t", "new-sub")).toBe(false);
    expect(belongsToSubscription({ ...snapshot, event: "thread.update", baseSeq: 1, seq: 2 }, "d", "t", "new-sub")).toBe(false);
    expect(belongsToSubscription({ ...snapshot, subscriptionId: "new-sub" }, "d", "t", "new-sub")).toBe(true);
    expect(belongsToSubscription({ ...snapshot, subscriptionId: "new-sub", deviceId: "other" }, "d", "t", "new-sub")).toBe(false);
    expect(belongsToSubscription({ ...snapshot, subscriptionId: "new-sub" }, "d", "t", "")).toBe(false);
  });

  it("retains a draft unless the executor accepted the send", () => {
    expect(preserveDraftAfterSend("hello", "rejected")).toBe("hello");
    expect(preserveDraftAfterSend("hello", "unknown")).toBe("hello");
    expect(preserveDraftAfterSend("hello", "accepted")).toBe("");
  });

  it("retains the selected device while offline and resumes only when it returns", () => {
    const previous = [{ deviceId: "desktop", agentOnline: true, codexReady: true }];
    expect(keepOfflineDevice([], previous, "desktop")).toEqual([{ deviceId: "desktop", agentOnline: false, codexReady: false }]);
    expect(recoveryTarget("desktop", "thread", [])).toBeNull();
    expect(recoveryTarget("desktop", "thread", [{ deviceId: "desktop" }])).toEqual({ deviceId: "desktop", threadId: "thread" });
    expect(recoveryTarget("desktop", "", [{ deviceId: "desktop" }])).toBeNull();
  });
});
