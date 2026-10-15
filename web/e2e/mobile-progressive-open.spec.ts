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
