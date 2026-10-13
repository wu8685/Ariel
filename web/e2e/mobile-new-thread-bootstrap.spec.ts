import { expect, test } from "@playwright/test";

const sessionToken = `s_${"b".repeat(64)}`;
const createdThread = {
  threadId: "00000000-0000-4000-8000-000000000096",
  title: "",
  cwd: "/tmp/new-project",
  updatedAt: "2026-10-13T00:00:00Z",
  runtime: "inProgress",
  turns: [{ turnId: "turn-first", status: "inProgress", items: [{ itemId: "user-first", role: "user", text: "hello", clientMessageId: "" }] }],
  pendingInteractions: [],
};

test("mobile local draft bootstraps once and keeps the composer stable", async ({ page }) => {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let bootstrapCount = 0;
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
        { deviceId: "mac", deviceName: "创建测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true, threadBootstrap: true } },
      ] } }));
    } else if (message.method === "thread.list") {
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { threads: [], nextCursor: "" } }));
    } else if (message.method === "thread.create") {
      createCount++;
    } else if (message.method === "thread.bootstrap") {
      bootstrapCount++;
      createdThread.turns[0].items[0].text = message.params.text;
      createdThread.turns[0].items[0].clientMessageId = message.params.clientMessageId;
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { thread: createdThread, threadId: createdThread.threadId, turnId: "turn-first", clientMessageId: message.params.clientMessageId } }));
    } else if (message.method === "thread.subscribe") {
      subscribeCount++;
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { subscriptionId: "created-sub" } }));
      socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: createdThread.threadId, subscriptionId: "created-sub", streamId: "created-stream", seq: 1, thread: createdThread }));
    }
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "新建会话" }).click();
  await page.getByRole("textbox", { name: "项目目录" }).fill(createdThread.cwd);
  await page.getByRole("button", { name: "创建会话" }).click();
  expect(createCount).toBe(0);
  expect(bootstrapCount).toBe(0);

  const composer = page.getByRole("textbox", { name: "发送消息" });
  await expect(composer).toBeEnabled();
  const before = await composer.boundingBox();
  await composer.fill("hello");
  const after = await composer.boundingBox();
  expect(after?.width).toBe(before?.width);
  expect(after?.height).toBeGreaterThanOrEqual(before?.height || 0);

  await page.getByRole("button", { name: "发送" }).click();
  await expect.poll(() => bootstrapCount).toBe(1);
  await expect.poll(() => subscribeCount).toBe(1);
  expect(createCount).toBe(0);
  await expect(page.getByRole("heading", { name: "未命名会话" })).toBeVisible();
  await expect(composer).toBeEnabled();
  await expect(composer).toHaveValue("");
  await expect(page.getByText("正在同步会话…")).toHaveCount(0);
});
