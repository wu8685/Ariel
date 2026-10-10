import { expect, test, type Page } from "@playwright/test";

const sessionToken = `s_${"b".repeat(64)}`;
const pinned = {
  threadId: "pinned-thread",
  title: "置顶的知识工程会话",
  cwd: "/Users/wuke/workspace/brain-spark",
  updatedAt: "2026-10-07T00:00:00Z",
  runtime: "idle",
  isPinned: true,
  turns: [],
  pendingInteractions: [],
};
const regular = {
  ...pinned,
  threadId: "regular-thread",
  title: "普通项目会话",
  isPinned: false,
};

async function openSessionFixture(page: Page, threads = [pinned, regular]) {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "sessions", relayEpoch: "sessions-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> | null = null;
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "会话测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads, nextCursor: "" };
    // Leave thread.subscribe pending so the real loading UI remains visible.
    if (data) socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
  }));
  await page.goto("/");
}

test("pinned sessions are visible before collapsed project groups", async ({ page }) => {
  await openSessionFixture(page);
  const pinnedRegion = page.getByRole("region", { name: "置顶会话" });
  const projectRegion = page.getByRole("region", { name: "项目 brain-spark" });
  await expect(pinnedRegion).toBeVisible();
  await expect(pinnedRegion).toContainText("置顶的知识工程会话");
  await expect(projectRegion).toBeVisible();
  await expect(projectRegion.getByRole("button", { name: /展开项目 brain-spark/ })).toHaveAttribute("aria-expanded", "false");
  await expect(page.locator('[data-thread-id="pinned-thread"]')).toHaveCount(1);
  const sidebar = page.locator(".sidebar");
  expect((await sidebar.locator(".thread-list").boundingBox())?.y).toBeLessThanOrEqual(190);
  await expect(sidebar.locator(".sidebar-head, .device-label, .device-meta")).toHaveCount(0);
  expect((await page.locator('[data-thread-id="pinned-thread"]').boundingBox())?.height).toBeLessThanOrEqual(52);
  await expect(page).toHaveScreenshot("mobile-pinned-sidebar.png", { animations: "disabled", caret: "hide" });
});

test("session loading mark uses the formal reverse micro asset rather than a color emoji", async ({ page }) => {
  await openSessionFixture(page, [regular]);
  await page.getByRole("button", { name: /展开项目 brain-spark/ }).click();
  await page.getByText("普通项目会话").click();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toBeVisible();
  const mark = page.locator(".empty-symbol .session-loading-logo.ariel-logo--micro");
  await expect(mark).toBeVisible();
  await expect(mark.locator("img")).toHaveAttribute("src", "/brand/ariel-logo-wind-messenger-micro-white.svg");
  await expect(mark).toHaveCSS("width", "50px");
  await expect(mark).toHaveCSS("height", "50px");
  await expect(page.locator(".empty-symbol")).not.toContainText("✳");
  await expect(page).toHaveScreenshot("mobile-session-loading-wind-messenger.png", { animations: "disabled", caret: "hide" });
});

test("a transient Agent reconnect keeps the selected conversation visible and read-only until a fresh snapshot arrives", async ({ page }) => {
  const reconnectThread = {
    ...regular,
    threadId: "reconnect-thread",
    title: "短暂重连会话",
    turns: [{ turnId: "turn-1", status: "completed", items: [{ itemId: "item-1", role: "assistant", text: "最后确认的会话内容" }] }],
  };
  let agentOnline = true;
  let subscriptions = 0;
  let pushEvent: (event: Record<string, unknown>) => void = () => { throw new Error("WebSocket fixture 尚未连接"); };

  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    pushEvent = event => socket.send(JSON.stringify(event));
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "reconnect", relayEpoch: "reconnect-epoch", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "device.list") {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "重连测试 Mac", agentOnline, codexReady: agentOnline, agentEpoch: agentOnline ? `agent-${subscriptions + 1}` : "agent-offline", capabilities: { autoLoad: true, send: true } }] } }));
      }
      if (message.method === "thread.list") {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { threads: [reconnectThread], nextCursor: "" } }));
      }
      if (message.method === "thread.subscribe") {
        subscriptions++;
        const subscriptionId = `sub-${subscriptions}`;
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { subscriptionId } }));
        socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: reconnectThread.threadId, subscriptionId, streamId: `stream-${subscriptions}`, seq: 1, thread: reconnectThread }));
      }
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: /展开项目 brain-spark/ }).click();
  await page.getByText(reconnectThread.title).click();
  await expect(page.getByText("最后确认的会话内容", { exact: true })).toBeVisible();

  agentOnline = false;
  pushEvent({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentOnline: false, codexReady: false });
  await expect(page.getByText("最后确认的会话内容", { exact: true })).toBeVisible();
  await expect(page.getByText(/Desktop Agent 正在重连/)).toBeVisible();
  await expect(page.getByRole("textbox", { name: "发送消息" })).toBeDisabled();
  await expect(page.getByText("Desktop Agent 暂时离线，恢复后会重新同步当前会话。")).toHaveCount(0);

  agentOnline = true;
  pushEvent({ type: "event", v: 1, event: "device.status", deviceId: "mac", agentOnline: true, codexReady: true });
  await expect.poll(() => subscriptions).toBe(2);
  await expect(page.getByText("最后确认的会话内容", { exact: true })).toBeVisible();
  await expect(page.getByText(/Desktop Agent 正在重连/)).toHaveCount(0);
  await expect(page.getByRole("textbox", { name: "发送消息" })).toBeEnabled();
});
