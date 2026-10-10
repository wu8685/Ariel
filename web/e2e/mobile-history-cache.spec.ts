import { expect, test } from "@playwright/test";
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
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "cache", relayEpoch: "relay-cache", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "缓存测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-cache", capabilities: { autoLoad: true, history: true } }] };
    if (message.method === "thread.list") data = { threads: [first, second], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: `sub-${message.params.threadId}` };
    if (message.method === "thread.history") {
      historyRequests++;
      data = {
        turns: [first.turns[0], { turnId: "older", status: "completed", items: [{ itemId: "older-item", role: "assistant", text: "只下载一次的旧历史" }] }],
        nextCursor: "",
      };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") {
      const thread = message.params.threadId === first.threadId ? first : second;
      socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: `sub-${thread.threadId}`, streamId: `stream-${thread.threadId}-${Date.now()}`, seq: 1, thread }));
    }
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 history-cache，/tmp/history-cache" }).click();
  await page.getByText(first.title).click();
  await expect(page.getByText("A 的最新内容")).toBeVisible();
  await page.getByRole("button", { name: "加载更早消息" }).click();
  await expect(page.getByText("只下载一次的旧历史")).toBeVisible();
  expect(historyRequests).toBe(1);

  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.getByText(second.title).click();
  await expect(page.getByText("B 正在运行")).toBeVisible();
  await expect(page.locator(".runtime")).toHaveText("运行中");

  const restoreLatest = page.getByRole("button", { name: "恢复输入区并回到最新" });
  if (await restoreLatest.isVisible()) await restoreLatest.click();
  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.getByText(first.title).click();
  await expect(page.getByText("只下载一次的旧历史")).toBeVisible();
  await expect(page.getByText("A 的最新内容")).toBeVisible();
  await expect(page.locator(".runtime")).toHaveText("待命");
  expect(historyRequests).toBe(1);

  await page.reload();
  await page.getByRole("button", { name: "展开项目 history-cache，/tmp/history-cache" }).click();
  await page.getByText(first.title).click();
  await expect(page.getByText("A 的最新内容")).toBeVisible();
  await page.getByRole("button", { name: "加载更早消息" }).click();
  await expect(page.getByText("只下载一次的旧历史")).toBeVisible();
  expect(historyRequests).toBe(2);
});
