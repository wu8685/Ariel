import { describe, expect, it } from "vitest";
import { applyThreadEvent, belongsToSubscription, preserveDraftAfterSend, recoveryTarget, keepOfflineDevice, permissionSummary, canSend, type ThreadView } from "./state";

const thread = { threadId: "t", title: "Same", cwd: "/a", updatedAt: "2026-10-04T00:00:00Z", runtime: "inProgress" as const, turns: [{ turnId: "turn", status: "inProgress" as const, items: [{ itemId: "assistant", role: "assistant" as const, text: "A" }] }], pendingInteractions: [] };
const snapshot = { type: "event" as const, v: 1 as const, event: "thread.snapshot" as const, deviceId: "d", threadId: "t", subscriptionId: "sub", streamId: "stream", seq: 1 as const, thread };

describe("thread stream state", () => {
  it("labels current Desktop permissions without claiming they match historical settings", () => {
    expect(permissionSummary({ sandbox: "full_access", approval: "on_request" })).toEqual({ label: "当前 Desktop：Full Access · 审批策略 on-request", warning: "Full Access 允许访问本机其他文件和网络；请确认这是当前 Desktop 允许的范围。" });
    expect(permissionSummary({ sandbox: "full_access", approval: "never" })).toEqual({ label: "当前 Desktop：Full Access · 审批策略 never", warning: "Full Access 允许访问本机其他文件和网络；请确认这是当前 Desktop 允许的范围。" });
    expect(permissionSummary({ sandbox: "read_only", approval: "on_request" }).label).toBe("当前 Desktop：Read Only · 审批策略 on-request");
    expect(permissionSummary(undefined).label).toBe("当前 Desktop 权限未知");
  });
  it("warns for Full Access but allows idle sends, while blocking pending interactions", () => {
    const idle = { ...thread, runtime: "idle" as const, turns: [], permissions: { sandbox: "full_access" as const, approval: "on_request" as const } };
    const readOnly = { ...idle, permissions: { sandbox: "read_only", approval: "on_request" } };
    expect(permissionSummary(idle.permissions).warning).toBeTruthy();
    expect(canSend(idle, true, false, "hello")).toBe(true);
    expect(canSend(readOnly, true, false, "hello")).toBe(true);
    expect(canSend({ ...idle, pendingInteractions: [{ interactionId: "pending" }] }, true, false, "hello")).toBe(false);
    expect(canSend(thread, true, false, "follow up", 0, true)).toBe(true);
    expect(canSend({ ...thread, pendingInteractions: [{ interactionId: "pending" }] }, true, false, "follow up", 0, true)).toBe(true);
    expect(canSend(thread, true, false, "follow up", 0, false)).toBe(false);
    expect(canSend(idle, false, false, "hello")).toBe(false);
  });
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
    expect(recoveryTarget("desktop", "thread", [{ deviceId: "desktop" }], "desktop\u0000thread")).toBeNull();
    expect(recoveryTarget("desktop", "", [{ deviceId: "desktop" }])).toBeNull();
  });
});
