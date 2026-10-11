import { expect, test } from "@playwright/test";

const sessionToken = `s_${"b".repeat(64)}`;
const createdThread = {
  threadId: "00000000-0000-4000-8000-000000000096",
  title: "",
  cwd: "/tmp/new-project",
  updatedAt: "2026-10-11T00:00:00Z",
  runtime: "idle",
  turns: [],
  pendingInteractions: [],
};

test("a newly created thread retries one cold subscription without duplicating creation", async ({ page }) => {
  await page.clock.install();
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let createCount = 0;
  let subscribeCount = 0;
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "create", relayEpoch: "create-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    if (message.method === "device.list") {
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { devices: [
        { deviceId: "mac", deviceName: "创建测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, threadCreate: true } },
      ] } }));
    } else if (message.method === "thread.list") {
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { threads: [], nextCursor: "" } }));
    } else if (message.method === "thread.create") {
      createCount++;
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { thread: createdThread } }));
    } else if (message.method === "thread.subscribe") {
      subscribeCount++;
      if (subscribeCount === 1) {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "rejected", error: { code: "RESYNC_REQUIRED", message: "not indexed yet" } }));
      } else {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { subscriptionId: "created-sub" } }));
        socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: createdThread.threadId, subscriptionId: "created-sub", streamId: "created-stream", seq: 1, thread: createdThread }));
      }
    }
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "新建会话" }).click();
  await page.getByRole("textbox", { name: "项目目录" }).fill(createdThread.cwd);
  await page.getByRole("button", { name: "创建会话" }).click();
  await expect.poll(() => subscribeCount).toBe(1);
  await expect(page.getByRole("alert")).toHaveCount(0);

  await page.clock.fastForward(500);
  await expect.poll(() => subscribeCount).toBe(2);
  expect(createCount).toBe(1);
  await expect(page.getByRole("heading", { name: "未命名会话" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "发送消息" })).toBeEnabled();
  await expect(page.getByRole("alert")).toHaveCount(0);
});
