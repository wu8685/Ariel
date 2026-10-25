import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"a".repeat(64)}`;
const cached: Thread = {
  threadId: "relay-hot",
  title: "Relay 热缓存",
  cwd: "/tmp/relay-hot-cache",
  updatedAt: "2026-10-25T00:00:00Z",
  runtime: "idle",
  historyComplete: false,
  recentComplete: true,
  turns: [
    { turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "user", text: "倒数第二个 turn" }] },
    { turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "Relay 中的最新结果" }] },
  ],
  pendingInteractions: [],
};

test("opens a fresh Relay cache immediately and keeps stale refresh non-destructive", async ({ page }) => {
  const methods: string[] = [];
  let socketSend: ((value: string) => void) | null = null;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    socketSend = value => socket.send(value);
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "cache", relayEpoch: "relay-cache", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      methods.push(message.method);
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "缓存测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-cache", capabilities: { autoLoad: true, history: true, send: true } }] };
      if (message.method === "thread.list") data = { threads: [cached], nextCursor: "" };
      if (message.method === "thread.cache.watch") data = { watchId: "watch-hot", freshness: "fresh", agentOnline: true, agentEpoch: "agent-cache", cacheStreamId: "cache-stream", seq: 1, capturedAt: "2026-10-25T00:00:00Z", thread: cached };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 relay-hot-cache，/tmp/relay-hot-cache" }).click();
  await page.getByText(cached.title).click();
  await expect(page.getByText("Relay 中的最新结果")).toBeVisible();
  await expect(page.getByText("倒数第二个 turn")).toBeVisible();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toHaveCount(0);
  await page.getByLabel("发送消息").fill("缓存命中后可发送");
  await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
  expect(methods.filter(method => method === "thread.read" || method === "thread.subscribe")).toEqual([]);

  socketSend!(JSON.stringify({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: cached.threadId, watchId: "watch-hot", freshness: "stale", staleReason: "agent_offline", agentOnline: false, agentEpoch: "agent-cache", cacheStreamId: "cache-stream", seq: 1, capturedAt: "2026-10-25T00:00:00Z", thread: cached }));
  await expect(page.getByText("Relay 中的最新结果")).toBeVisible();
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toHaveCount(0);

  const refreshed: Thread = {
    ...cached,
    updatedAt: "2026-10-25T00:00:01Z",
    turns: [
      cached.turns[1],
      { turnId: "new-latest", status: "completed", items: [{ itemId: "new-latest-item", role: "assistant", text: "后台更新后的最新结果" }] },
    ],
  };
  socketSend!(JSON.stringify({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: cached.threadId, watchId: "watch-hot", freshness: "fresh", agentOnline: true, agentEpoch: "agent-cache", cacheStreamId: "cache-stream", seq: 2, capturedAt: "2026-10-25T00:00:01Z", thread: refreshed }));
  await expect(page.getByText("后台更新后的最新结果")).toBeVisible();
  await expect(page.getByText("倒数第二个 turn")).toBeVisible();
  await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
});

test("shows bounded history during a cold cache promotion and atomically hands off to owner", async ({ page }) => {
  const methods: string[] = [];
  let socketSend: ((value: string) => void) | null = null;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    socketSend = value => socket.send(value);
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "cache-cold", relayEpoch: "relay-cache", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      methods.push(message.method);
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "缓存测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-cache", capabilities: { autoLoad: true, history: true, send: true } }] };
      if (message.method === "thread.list") data = { threads: [cached], nextCursor: "" };
      if (message.method === "thread.cache.watch") data = { watchId: "watch-cold", freshness: "missing", agentOnline: true, agentEpoch: "agent-cache" };
      if (message.method === "thread.read") data = { thread: cached };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 relay-hot-cache，/tmp/relay-hot-cache" }).click();
  await page.getByText(cached.title).click();
  await expect(page.getByText("Relay 中的最新结果")).toBeVisible();
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toHaveCount(0);
  expect(methods.filter(method => method === "thread.read")).toEqual(["thread.read"]);
  expect(methods.filter(method => method === "thread.subscribe")).toEqual([]);

  const refreshed: Thread = {
    ...cached,
    updatedAt: "2026-10-25T00:00:01Z",
    turns: [cached.turns[1], { turnId: "owner", status: "completed", items: [{ itemId: "owner-item", role: "assistant", text: "Owner 后台接管完成" }] }],
  };
  socketSend!(JSON.stringify({ type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: cached.threadId, watchId: "watch-cold", freshness: "fresh", agentOnline: true, agentEpoch: "agent-cache", cacheStreamId: "cache-stream", seq: 1, capturedAt: "2026-10-25T00:00:01Z", thread: refreshed }));
  await expect(page.getByText("Owner 后台接管完成")).toBeVisible();
  await page.getByLabel("发送消息").fill("接管后可发送");
  await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
});
