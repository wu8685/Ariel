import { expect, test, type WebSocketRoute } from "@playwright/test";
import type { Thread, Turn } from "../src/generated/protocol";

const sessionToken = `s_${"e".repeat(64)}`;
const turn = (id: string): Turn => ({ turnId: id, status: "completed", items: [{ itemId: `${id}-item`, role: "assistant", text: id }] });
const stored: Thread = {
  threadId: "progressive-open",
  title: "渐进打开",
  cwd: "/tmp/progressive-open",
  updatedAt: "2026-10-15T00:00:00Z",
  runtime: "idle",
  historyComplete: true,
  recentComplete: true,
  turns: [turn("turn-stored-1"), turn("turn-stored-2")],
  pendingInteractions: [],
};

// Spec 0086: the persisted-history fast path must render before the owner
// cold sync finishes, keep every mutation disabled, and hand over to the
// fresh snapshot without blanking the transcript.
test("renders persisted history while the owner snapshot is still loading, then hands over", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let ws: WebSocketRoute | null = null;
  let pendingSubscribeId = "";
  await page.routeWebSocket("**/ws", socket => {
    ws = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "progressive", relayEpoch: "relay-progressive", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "thread.subscribe") {
        // Hold the owner sync back until the fast path has rendered.
        pendingSubscribeId = message.requestId;
        return;
      }
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Progressive Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-progressive", capabilities: { autoLoad: true, history: true } }] };
      if (message.method === "thread.list") data = { threads: [stored], nextCursor: "" };
      if (message.method === "thread.read") data = { thread: stored };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 progressive-open，/tmp/progressive-open" }).click();
  await page.getByText(stored.title).click();

  // Fast path: history renders while the owner sync is still pending.
  await expect(page.getByText("turn-stored-1")).toBeVisible();
  await expect(page.getByText("turn-stored-2")).toBeVisible();
  await expect(page.getByText(/正在同步最新状态.*操作暂时禁用/)).toBeVisible();
  await page.getByRole("textbox", { name: "发送消息" }).fill("同步中不可发送");
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  await expect.poll(() => pendingSubscribeId).not.toBe("");

  // Slow path completes: the fresh owner snapshot takes over without blanking.
  const fresh: Thread = { ...stored, turns: [...stored.turns, turn("turn-live")] };
  ws!.send(JSON.stringify({ type: "response", v: 1, requestId: pendingSubscribeId, outcome: "accepted", data: { subscriptionId: "sub-progressive" } }));
  ws!.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: stored.threadId, subscriptionId: "sub-progressive", streamId: "stream-progressive", seq: 1, thread: fresh }));

  await expect(page.getByText("turn-live")).toBeVisible();
  await expect(page.getByText("turn-stored-1")).toBeVisible();
  await expect(page.getByText(/正在同步最新状态/)).toHaveCount(0);
  await expect(page.getByText(/历史只读/)).toHaveCount(0);
  await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
});

// Spec 0086: when the owner sync fails after the fast path rendered, the
// persisted history stays read-only instead of dropping to a blank page.
test("keeps read-only history when the owner sync times out", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let ws: WebSocketRoute | null = null;
  let pendingSubscribeId = "";
  await page.routeWebSocket("**/ws", socket => {
    ws = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "progressive-2", relayEpoch: "relay-progressive-2", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "thread.subscribe") {
        pendingSubscribeId = message.requestId;
        return;
      }
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Progressive Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-progressive-2", capabilities: { autoLoad: true, history: true } }] };
      if (message.method === "thread.list") data = { threads: [stored], nextCursor: "" };
      if (message.method === "thread.read") data = { thread: stored };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 progressive-open，/tmp/progressive-open" }).click();
  await page.getByText(stored.title).click();
  await expect(page.getByText("turn-stored-1")).toBeVisible();
  await expect.poll(() => pendingSubscribeId).not.toBe("");

  ws!.send(JSON.stringify({ type: "response", v: 1, requestId: pendingSubscribeId, outcome: "rejected", error: { code: "TIMEOUT", message: "TIMEOUT" } }));

  await expect(page.getByText("turn-stored-1")).toBeVisible();
  await expect(page.getByText(/历史只读.*Desktop 状态未确认/)).toBeVisible();
  await expect(page.getByRole("button", { name: "发送" })).toBeDisabled();
});

// Spec 0099: a subscription timeout must not invalidate the persisted-history
// request that was already in flight for the same selection.
test("renders a late persisted history response after the owner sync times out", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let ws: WebSocketRoute | null = null;
  let pendingSubscribeId = "";
  let pendingReadId = "";
  await page.routeWebSocket("**/ws", socket => {
    ws = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "progressive-late", relayEpoch: "relay-progressive-late", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "thread.subscribe") { pendingSubscribeId = message.requestId; return; }
      if (message.method === "thread.read") { pendingReadId = message.requestId; return; }
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Progressive Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-progressive-late", capabilities: { autoLoad: true, history: true } }] };
      if (message.method === "thread.list") data = { threads: [stored], nextCursor: "" };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 progressive-open，/tmp/progressive-open" }).click();
  await page.getByText(stored.title).click();
  await expect.poll(() => pendingSubscribeId).not.toBe("");
  await expect.poll(() => pendingReadId).not.toBe("");

  ws!.send(JSON.stringify({ type: "response", v: 1, requestId: pendingSubscribeId, outcome: "rejected", error: { code: "TIMEOUT", message: "TIMEOUT" } }));
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toBeVisible();
  ws!.send(JSON.stringify({ type: "response", v: 1, requestId: pendingReadId, outcome: "accepted", data: { thread: stored } }));

  await expect(page.getByText("turn-stored-1")).toBeVisible();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toHaveCount(0);
  await expect(page.getByText(/历史只读.*Desktop 状态未确认/)).toBeVisible();
  await expect(page.getByRole("button", { name: "发送" })).toBeDisabled();
  await expect(page.getByRole("alert")).not.toContainText("不要直接重发");
});

// Spec 0093: temporary large-follower pressure must be distinguishable from
// a permanently oversized owner, and a user retry reuses visible history.
test("retries an overloaded owner without blanking or rereading history", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let ws: WebSocketRoute | null = null;
  let subscribeCount = 0;
  let readCount = 0;
  let retrySubscribeId = "";
  await page.routeWebSocket("**/ws", socket => {
    ws = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "progressive-3", relayEpoch: "relay-progressive-3", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "thread.subscribe") {
        subscribeCount++;
        if (subscribeCount === 1) socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "rejected", error: { code: "OVERLOADED", message: "OVERLOADED" } }));
        else retrySubscribeId = message.requestId;
        return;
      }
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Progressive Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-progressive-3", capabilities: { autoLoad: true, history: true } }] };
      if (message.method === "thread.list") data = { threads: [stored], nextCursor: "" };
      if (message.method === "thread.read") { readCount++; data = { thread: stored }; }
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 progressive-open，/tmp/progressive-open" }).click();
  await page.getByText(stored.title).click();
  await expect(page.getByText("turn-stored-1")).toBeVisible();
  await expect(page.getByText(/实时连接资源繁忙.*发送暂时禁用/)).toBeVisible();

  await page.getByRole("button", { name: "重新连接" }).click();
  await expect.poll(() => retrySubscribeId).not.toBe("");
  await expect.poll(() => readCount).toBe(1);
  await expect(page.getByText("turn-stored-1")).toBeVisible();
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();

  const fresh: Thread = { ...stored, turns: [...stored.turns, turn("turn-reconnected")] };
  ws!.send(JSON.stringify({ type: "response", v: 1, requestId: retrySubscribeId, outcome: "accepted", data: { subscriptionId: "sub-retry" } }));
  ws!.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: stored.threadId, subscriptionId: "sub-retry", streamId: "stream-retry", seq: 1, thread: fresh }));
  await expect(page.getByText("turn-reconnected")).toBeVisible();
  await expect(page.getByText(/历史只读/)).toHaveCount(0);
  await page.getByRole("textbox", { name: "发送消息" }).fill("连接恢复后可发送");
  await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
});
