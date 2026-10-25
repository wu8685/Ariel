import { expect, test, type WebSocketRoute } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"a".repeat(64)}`;
const first: Thread = {
  threadId: "cache-first",
  title: "缓存会话 A",
  cwd: "/tmp/history-cache",
  updatedAt: "2026-10-10T00:00:00Z",
  runtime: "idle",
  historyComplete: false,
  recentComplete: true,
  turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "A 的最新内容" }] }],
  pendingInteractions: [],
};
const second: Thread = {
  ...first,
  threadId: "cache-second",
  title: "缓存会话 B",
  runtime: "inProgress",
  turns: [{ turnId: "active", status: "inProgress", items: [{ itemId: "active-item", role: "assistant", text: "B 正在运行" }] }],
};

test("reuses downloaded history after a mobile user switches away and back", async ({ page }) => {
  let historyRequests = 0;
  let connections = 0;
  let subscriptions = 0;
  let relaySocket: WebSocketRoute | null = null;
  const historyLimits: number[] = [];
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    connections++;
    relaySocket = socket;
    socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "cache", relayEpoch: "relay-cache", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "缓存测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-cache", capabilities: { autoLoad: true, history: true } }] };
    if (message.method === "thread.list") data = { threads: [first, second], nextCursor: "" };
    if (message.method === "thread.subscribe") {
      subscriptions++;
      data = { subscriptionId: `sub-${message.params.threadId}` };
    }
    if (message.method === "thread.history") {
      historyRequests++;
      historyLimits.push(message.params.limit);
      data = {
        turns: [first.turns[0], { turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "assistant", text: "只下载一次的旧历史" }] }],
        nextCursor: "",
      };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") {
      const thread = message.params.threadId === first.threadId ? first : second;
      const sendSnapshot = () => socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: `sub-${thread.threadId}`, streamId: `stream-${thread.threadId}`, seq: 1, thread }));
      if (connections > 1 && thread.threadId === first.threadId) setTimeout(sendSnapshot, 800);
      else sendSnapshot();
    }
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 history-cache，/tmp/history-cache" }).click();
  await page.getByText(first.title).click();
  await expect(page.getByText("A 的最新内容")).toBeVisible();
  const composer = page.getByLabel("发送消息");
  await composer.fill("应用切换后仍保留");
  const connectionsBeforeForeground = connections;
  const subscriptionsBeforeForeground = subscriptions;
  await page.evaluate(() => {
    let state = "hidden";
    let now = 1_000;
    const originalNow = Date.now;
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => state });
    Date.now = () => now;
    document.dispatchEvent(new Event("visibilitychange"));
    state = "visible";
    now = 7_000;
    document.dispatchEvent(new Event("visibilitychange"));
    Date.now = originalNow;
  });
  await expect(composer).toHaveValue("应用切换后仍保留");
  expect(connections).toBe(connectionsBeforeForeground);
  expect(subscriptions).toBe(subscriptionsBeforeForeground);
  await composer.fill("");
  await page.getByRole("button", { name: "加载更早消息" }).click();
  await expect(page.getByText("只下载一次的旧历史")).toBeVisible();
  expect(historyRequests).toBe(1);
  expect(historyLimits).toEqual([3]);

  await composer.fill("真实断线也保留");
  const readySendBox = await page.getByRole("button", { name: "发送" }).boundingBox();
  expect(readySendBox).not.toBeNull();
  relaySocket!.close({ code: 1012, reason: "fixture reconnect" });
  await expect(page.getByText("A 的最新内容")).toBeVisible();
  await expect(composer).toHaveValue("真实断线也保留");
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toHaveCount(0);
  const reconnectingSend = page.getByRole("button", { name: "正在同步会话，暂不可发送" });
  await expect(reconnectingSend).toBeDisabled();
  await expect(reconnectingSend.locator(".send-loading-spinner")).toBeVisible();
  const reconnectingBox = await reconnectingSend.boundingBox();
  expect(reconnectingBox).not.toBeNull();
  expect(reconnectingBox!.width).toBeCloseTo(readySendBox!.width, 1);
  expect(reconnectingBox!.height).toBeCloseTo(readySendBox!.height, 1);
  expect(reconnectingBox!.x).toBeCloseTo(readySendBox!.x, 1);
  expect(reconnectingBox!.y).toBeCloseTo(readySendBox!.y, 1);
  await expect(page.locator(".composer")).toHaveScreenshot("background-sync-send-loading.png", { animations: "disabled" });
  await expect.poll(() => connections).toBe(2);
  await expect(page.getByText("A 的最新内容")).toBeVisible();
  await expect(composer).toHaveValue("真实断线也保留");
  await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
  await composer.fill("");

  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.getByText(second.title).click();
  await expect(page.getByText("B 正在运行")).toBeVisible();
  await expect(page.locator(".runtime")).toHaveText("运行中");
  const firstUpdated: Thread = { ...first, updatedAt: "2026-10-10T00:00:01Z", turns: [{ ...first.turns[0], items: [{ ...first.turns[0].items[0], text: "A 的后台增量" }] }] };
  relaySocket!.send(JSON.stringify({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: first.threadId, subscriptionId: `sub-${first.threadId}`, streamId: `stream-${first.threadId}`, baseSeq: 1, seq: 2, thread: firstUpdated }));
  await expect(page.getByText("B 正在运行")).toBeVisible();

  const restoreLatest = page.getByRole("button", { name: "恢复输入区并回到最新" });
  if (await restoreLatest.isVisible()) await restoreLatest.click();
  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.getByText(first.title).click();
  await expect(page.getByText("只下载一次的旧历史")).toBeVisible();
  await expect(page.getByText("A 的后台增量")).toBeVisible();
  await expect(page.locator(".runtime")).toHaveText("待命");
  await expect(page.getByText(/正在同步最新状态/)).toHaveCount(0);
  expect(subscriptions).toBe(3);
  expect(historyRequests).toBe(1);

  await page.reload();
  await expect(page.getByText("A 的后台增量")).toBeVisible();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toHaveCount(0);
  await expect(page.getByText("只下载一次的旧历史")).toBeVisible();
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  expect(historyRequests).toBe(1);
  await expect.poll(() => subscriptions).toBe(4);
  await expect(page.getByText("A 的最新内容")).toBeVisible();
  await expect(page.getByRole("button", { name: "发送" })).toBeVisible();
  await expect(page.getByText("只下载一次的旧历史")).toBeVisible();
  expect(historyRequests).toBe(1);
  expect(historyLimits).toEqual([3]);
});
