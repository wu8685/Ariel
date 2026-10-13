import { expect, test } from "@playwright/test";

const sessionToken = `s_${"d".repeat(64)}`;
const first = {
  threadId: "read-thread",
  title: "已经查看的会话",
  cwd: "/Users/wuke/workspace/Ariel",
  updatedAt: "2026-10-13T00:00:00Z",
  runtime: "idle",
  turns: [],
  pendingInteractions: [],
};
const second = { ...first, threadId: "unread-thread", title: "后台完成的会话" };

test("a batch update shows one local unread dot and rendered selection clears it", async ({ page }) => {
  let listCalls = 0;
  const changed = { ...second, updatedAt: "2026-10-13T00:00:01Z", turns: [{ turnId: "turn", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "后台任务已经完成。" }] }] };
  await page.addInitScript(token => {
    sessionStorage.setItem("ariel.web-session.v1", token);
    localStorage.clear();
  }, sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "unread", relayEpoch: "unread-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    if (message.method === "device.list") {
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "未读测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-unread", capabilities: { autoLoad: true } }] } }));
      return;
    }
    if (message.method === "thread.list") {
      listCalls++;
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { threads: listCalls === 1 ? [first, second] : [first, changed], nextCursor: "" } }));
      return;
    }
    if (message.method === "thread.subscribe" && message.params.threadId === second.threadId) {
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { subscriptionId: "sub-unread" } }));
      socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: second.threadId, subscriptionId: "sub-unread", streamId: "stream-unread", seq: 1, thread: changed }));
    }
  }));

  await page.goto("/");
  await page.getByRole("button", { name: /展开项目 Ariel/ }).click();
  await expect(page.getByLabel("有新结果")).toHaveCount(0);
  await page.getByRole("button", { name: "刷新会话" }).click();
  const unreadRow = page.locator('[data-thread-id="unread-thread"]');
  const dot = unreadRow.getByLabel("有新结果");
  await expect(dot).toBeVisible();
  expect((await dot.boundingBox())?.width).toBe(7);
  expect((await dot.boundingBox())?.height).toBe(7);
  await expect(page).toHaveScreenshot("mobile-local-unread-dot.png", { animations: "disabled", caret: "hide" });

  await unreadRow.click();
  await expect(page.getByText("后台任务已经完成。", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: /打开会话列表/ }).click();
  await expect(page.locator('[data-thread-id="unread-thread"]').getByLabel("有新结果")).toHaveCount(0);
  expect(listCalls).toBeGreaterThanOrEqual(3);
});
