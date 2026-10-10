import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"b".repeat(64)}`;
const thread: Thread = {
  threadId: "reconnect-thread",
  title: "重连恢复会话",
  cwd: "/tmp/reconnect-fixture",
  updatedAt: "2026-10-10T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "latest-item", role: "assistant", text: "已恢复的新快照" }] }],
  pendingInteractions: [],
};

test("a new Agent epoch recovers once and fences late events from the departed epoch", async ({ page }) => {
  let relaySocket: { send(data: string): void } | null = null;
  let online = true;
  let epoch = "epoch-old";
  let subscriptions = 0;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    relaySocket = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "reconnect", relayEpoch: "relay", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: online ? [{ deviceId: "mac", deviceName: "Reconnect Mac", agentOnline: true, codexReady: true, agentEpoch: epoch, capabilities: { autoLoad: true } }] : [] };
      if (message.method === "thread.list") data = { threads: [thread], nextCursor: "" };
      if (message.method === "thread.subscribe") {
        subscriptions++;
        data = { subscriptionId: `sub-${subscriptions}` };
      }
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
      if (message.method === "thread.subscribe") {
        socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: `sub-${subscriptions}`, streamId: `stream-${subscriptions}`, seq: 1, thread }));
      }
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 reconnect-fixture，/tmp/reconnect-fixture" }).click();
  await page.getByText(thread.title).click();
  await expect(page.getByText("已恢复的新快照")).toBeVisible();
  await page.getByLabel("发送消息").fill("断线期间保留的草稿");
  expect(subscriptions).toBe(1);

  online = false;
  relaySocket!.send(JSON.stringify({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: "epoch-old", agentOnline: false, codexReady: false }));
  await expect(page.getByText("已恢复的新快照")).toBeVisible();
  await expect(page.getByText(/Desktop Agent 正在重连/)).toBeVisible();
  await expect(page.getByLabel("发送消息")).toBeDisabled();
  await expect(page.getByText("Desktop Agent 暂时离线，恢复后会重新同步当前会话。")).toHaveCount(0);

  online = true;
  epoch = "epoch-new";
  relaySocket!.send(JSON.stringify({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: "epoch-new", agentOnline: true, codexReady: true }));
  relaySocket!.send(JSON.stringify({ type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub-1", streamId: "stream-1", baseSeq: 1, seq: 2, thread: { ...thread, runtime: "inProgress" } }));
  relaySocket!.send(JSON.stringify({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentEpoch: "epoch-old", agentOnline: false, codexReady: false }));

  await expect.poll(() => subscriptions).toBe(2);
  await expect(page.getByText("已恢复的新快照")).toBeVisible();
  await expect(page.getByLabel("发送消息")).toHaveValue("断线期间保留的草稿");
  await expect(page.getByLabel("发送消息")).toBeEnabled();
  await expect(page.getByText(/Desktop Agent 正在重连/)).toHaveCount(0);
  await expect(page.getByText("Desktop Agent 暂时离线，恢复后会重新同步当前会话。")).toHaveCount(0);
  await expect(page.getByText("事件顺序中断，正在重新同步会话…")).toHaveCount(0);
  expect(subscriptions).toBe(2);
});
