import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"d".repeat(64)}`;
const first: Thread = { threadId: "draft-a", title: "草稿会话 A", cwd: "/tmp/drafts", updatedAt: "2026-10-17T00:00:00Z", runtime: "idle", turns: [], pendingInteractions: [] };
const second: Thread = { ...first, threadId: "draft-b", title: "草稿会话 B" };

test("restores a separate draft for every conversation without flashing the previous text", async ({ page }) => {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "drafts", relayEpoch: "relay", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Draft Mac", agentOnline: true, codexReady: true, agentEpoch: "agent", capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [first, second], nextCursor: "" };
    if (message.method === "thread.read") data = { thread: message.params.threadId === first.threadId ? first : second };
    if (message.method === "thread.subscribe") data = { subscriptionId: `sub-${message.params.threadId}` };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") {
      const thread = message.params.threadId === first.threadId ? first : second;
      socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: `sub-${thread.threadId}`, streamId: `stream-${thread.threadId}`, seq: 1, thread }));
    }
  }));

  await page.goto("/");
  await page.getByRole("button", { name: /展开项目 drafts/ }).click();
  await page.locator('[data-thread-id="draft-a"]').click();
  const composer = page.getByLabel("发送消息");
  await composer.fill("只属于 A 的草稿");

  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.locator('[data-thread-id="draft-b"]').click();
  await expect(composer).toHaveValue("");
  await composer.fill("只属于 B 的草稿");

  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.locator('[data-thread-id="draft-a"]').click();
  await expect(composer).toHaveValue("只属于 A 的草稿");

  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.locator('[data-thread-id="draft-b"]').click();
  await expect(composer).toHaveValue("只属于 B 的草稿");
});

test("a late send receipt clears its source draft without clearing the newly selected conversation", async ({ page }) => {
  let relaySocket: { send(data: string): void } | null = null;
  let sendRequest: Record<string, any> | null = null;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    relaySocket = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "draft-send", relayEpoch: "relay", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "turn.start") { sendRequest = message; return; }
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Draft Mac", agentOnline: true, codexReady: true, agentEpoch: "agent", capabilities: { autoLoad: true, send: true } }] };
      if (message.method === "thread.list") data = { threads: [first, second], nextCursor: "" };
      if (message.method === "thread.read") data = { thread: message.params.threadId === first.threadId ? first : second };
      if (message.method === "thread.subscribe") data = { subscriptionId: `sub-${message.params.threadId}` };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
      if (message.method === "thread.subscribe") {
        const thread = message.params.threadId === first.threadId ? first : second;
        socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: `sub-${thread.threadId}`, streamId: `stream-${thread.threadId}`, seq: 1, thread }));
      }
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: /展开项目 drafts/ }).click();
  await page.locator('[data-thread-id="draft-a"]').click();
  const composer = page.getByLabel("发送消息");
  await composer.fill("正在发送的 A");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect.poll(() => sendRequest?.method).toBe("turn.start");

  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.locator('[data-thread-id="draft-b"]').click();
  await composer.fill("回执到达前填写的 B");
  relaySocket!.send(JSON.stringify({ type: "response", v: 1, requestId: sendRequest!.requestId, outcome: "accepted", data: { turnId: "accepted" } }));

  await expect(composer).toHaveValue("回执到达前填写的 B");
  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await page.locator('[data-thread-id="draft-a"]').click();
  await expect(composer).toHaveValue("");
});
