import { describe, expect, it } from "vitest";
import { isProactiveTaskMessage, shouldSurfaceNotification } from "./taskNotification";

describe("task notifications", () => {
  it("only treats finished task-event messages as proactive", () => {
    expect(isProactiveTaskMessage({ kind: "text", cardJSON: JSON.stringify({ proactive: true, source: "task_event" }) })).toBe(true);
    expect(isProactiveTaskMessage({ kind: "text", cardJSON: JSON.stringify({ proactive: true, source: "notify_user" }) })).toBe(true);
    expect(isProactiveTaskMessage({ kind: "processing", cardJSON: JSON.stringify({ proactive: true, source: "task_event" }) })).toBe(false);
    expect(isProactiveTaskMessage({ kind: "text", cardJSON: JSON.stringify({ proactive: true, source: "user" }) })).toBe(false);
    expect(isProactiveTaskMessage({ kind: "text", cardJSON: "not-json" })).toBe(false);
  });

  it("shows a system notification only when a watch result arrives and the page is in the background", () => {
    expect(shouldSurfaceNotification({ proactive: true, focused: false, permission: "granted", enabled: true })).toBe(true);
    expect(shouldSurfaceNotification({ proactive: true, focused: true, permission: "granted", enabled: true })).toBe(false);
    expect(shouldSurfaceNotification({ proactive: true, focused: false, permission: "default", enabled: true })).toBe(false);
    expect(shouldSurfaceNotification({ proactive: true, focused: false, permission: "granted", enabled: false })).toBe(false);
    expect(shouldSurfaceNotification({ proactive: false, focused: false, permission: "granted", enabled: true })).toBe(false);
  });
});
