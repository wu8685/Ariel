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

test("late retired cache errors cannot force a recovered mobile conversation to reconnect", async ({ page }) => {
  const methods: string[] = [];
  let send: (value: string) => void = () => { throw new Error("socket not ready"); };
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    send = value => socket.send(value);
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "cache-order", relayEpoch: "relay-cache", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      methods.push(message.method);
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "缓存测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-cache", capabilities: { autoLoad: true, send: true } }] };
      if (message.method === "thread.list") data = { threads: [cached] };
      if (message.method === "thread.cache.watch") data = { watchId: "watch-order", freshness: "fresh", agentOnline: true, agentEpoch: "agent-cache", cacheStreamId: "old", seq: 1, capturedAt: cached.updatedAt, thread: cached };
      send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 relay-hot-cache，/tmp/relay-hot-cache" }).click();
  await page.getByText(cached.title).click();
  await expect(page.getByText("Relay 中的最新结果")).toBeVisible();
  const draft = page.getByLabel("发送消息");
  await draft.fill("恢复后保留草稿，不实际发送");
  const update = { type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: cached.threadId, watchId: "watch-order", freshness: "fresh", agentOnline: true, agentEpoch: "agent-cache", cacheStreamId: "new", seq: 3, capturedAt: cached.updatedAt, thread: cached };
  send(JSON.stringify(update));
  // A visible content barrier proves the replacement stream has been consumed.
  const recovered = { ...cached, turns: [...cached.turns, { turnId: "recovered", status: "completed", items: [{ itemId: "recovered-item", role: "assistant", text: "新 stream 正常接管" }] }] };
  send(JSON.stringify({ ...update, seq: 4, thread: recovered }));
  await expect(page.getByText("新 stream 正常接管")).toBeVisible();
  for (const [cacheStreamId, seq] of [["old", 99], ["new", 3]] as const) {
    send(JSON.stringify({ ...update, cacheStreamId, seq, freshness: "stale", staleReason: "RESYNC_REQUIRED" }));
    // A later harmless event on the same socket is a DOM barrier: negative
    // assertions must not pass before React consumes the delayed status.
    send(JSON.stringify({ type: "event", v: 1, event: "interaction.resolved", deviceId: "mac", threadId: cached.threadId, interactionId: "barrier" }));
    await expect(page.getByText("交互已完成，会话状态已同步。")).toBeVisible();
    await expect(page.locator(".readonly-banner")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "重新连接", exact: true })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
    await expect(draft).toBeEditable();
    await expect(draft).toHaveValue("恢复后保留草稿，不实际发送");
    await expect(page.getByText("新 stream 正常接管")).toBeVisible();
    await page.getByRole("button", { name: "关闭提示", exact: true }).click();
  }
  send(JSON.stringify({ ...update, seq: 4, freshness: "stale", staleReason: "RESYNC_REQUIRED" }));
  await expect(page.getByRole("button", { name: "重新连接", exact: true })).toBeVisible();
  await expect(page.locator(".send-button")).toBeDisabled();
  send(JSON.stringify({ ...update, seq: 5, thread: recovered }));
  await expect(page.locator(".readonly-banner")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  await expect(draft).toHaveValue("恢复后保留草稿，不实际发送");
  expect(methods.filter(method => /^(turn|queue|interaction)\./.test(method))).toEqual([]);
  expect(methods.filter(method => method === "thread.cache.promote")).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test("a delayed cold history read cannot erase a stale owner's lineage or block same-seq lease recovery", async ({ page }) => {
  let send: (value: string) => void = () => { throw new Error("socket not ready"); };
  let readID = "";
  const methods: string[] = [];
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    send = value => socket.send(value);
    socket.onMessage(raw => {
      const m = JSON.parse(String(raw));
      if (m.type === "hello") { send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "cold-order", relayEpoch: "relay", sessionToken })); return; }
      if (m.type !== "request") return;
      methods.push(m.method);
      let data: Record<string, unknown> = {};
      if (m.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "缓存测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-cache", capabilities: { autoLoad: true, send: true } }] };
      if (m.method === "thread.list") data = { threads: [cached] };
      if (m.method === "thread.cache.watch") data = { watchId: "watch-cold-order", freshness: "missing", agentOnline: true, agentEpoch: "agent-cache" };
      if (m.method === "thread.read") { readID = m.requestId; return; }
      send(JSON.stringify({ type: "response", v: 1, requestId: m.requestId, outcome: "accepted", data }));
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 relay-hot-cache，/tmp/relay-hot-cache" }).click();
  await page.getByText(cached.title).click();
  await expect.poll(() => readID).not.toBe("");
  const owner: Thread = { ...cached, turns: [{ turnId: "owner", status: "completed", items: [{ itemId: "owner-item", role: "assistant", text: "Owner 的较新正文不能被旧 read 覆盖" }] }] };
  const update = { type: "event", v: 1, event: "thread.cache.update", deviceId: "mac", threadId: cached.threadId, watchId: "watch-cold-order", freshness: "fresh", agentOnline: true, agentEpoch: "agent-cache", cacheStreamId: "owner-stream", seq: 1, capturedAt: cached.updatedAt, thread: owner };
  send(JSON.stringify(update));
  await expect(page.getByText("Owner 的较新正文不能被旧 read 覆盖")).toBeVisible();
  const draft = page.getByLabel("发送消息");
  await draft.fill("同 seq 恢复保留的草稿");
  send(JSON.stringify({ ...update, freshness: "stale", staleReason: "follower_lease_expired" }));
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  send(JSON.stringify({ type: "response", v: 1, requestId: readID, outcome: "accepted", data: { thread: cached } }));
  send(JSON.stringify({ type: "event", v: 1, event: "interaction.resolved", deviceId: "mac", threadId: cached.threadId, interactionId: "barrier" }));
  await expect(page.getByText("交互已完成，会话状态已同步。")).toBeVisible();
  await expect(page.getByText("Owner 的较新正文不能被旧 read 覆盖")).toBeVisible();
  await expect(page.getByText("Relay 中的最新结果")).toHaveCount(0);
  send(JSON.stringify(update));
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  await expect(page.locator(".readonly-banner")).toHaveCount(0);
  await expect(draft).toHaveValue("同 seq 恢复保留的草稿");
  expect(methods.filter(method => /^(turn|queue|interaction)\./.test(method))).toEqual([]);
});

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
