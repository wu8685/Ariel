import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

test.use({ viewport: { width: 1280, height: 720 }, deviceScaleFactor: 1, isMobile: false, hasTouch: false });

const sessionToken = `s_${"b".repeat(64)}`;
const fixtureThread: Thread = {
  threadId: "desktop-unified",
  title: "跨端统一界面验收",
  cwd: "/tmp/desktop-unified",
  updatedAt: "2026-10-08T00:00:00Z",
  runtime: "inProgress",
  permissions: { sandbox: "full_access", approval: "on_request" },
  turns: [{ turnId: "active-turn", status: "inProgress", items: [{ itemId: "assistant", role: "assistant", text: "正在执行桌面端视觉验收。" }] }],
  pendingInteractions: [],
  queuedMessages: [{ queueId: "queued", clientMessageId: "queued-message", text: "补充检查桌面端 icon", images: [], editable: true }],
};

async function openFixture(page: Page) {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "desktop-ui", relayEpoch: "desktop-ui-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "桌面视觉测试", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, queue: true, threadCreate: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "desktop-sub" };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: fixtureThread.threadId, subscriptionId: "desktop-sub", streamId: "desktop-stream", seq: 1, thread: fixtureThread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 desktop-unified，/tmp/desktop-unified" }).click();
  await page.getByText(fixtureThread.title).click();
  await expect(page.getByRole("heading", { name: fixtureThread.title })).toBeVisible();
}

test("desktop reuses the compact mobile shell and icon controls", async ({ page }) => {
  await openFixture(page);

  await expect(page.locator(".masthead")).toBeHidden();
  await expect(page.locator(".conversation-head")).toHaveCSS("height", "66px");
  await expect(page.locator(".permission-strip")).toBeHidden();
  await expect(page.locator(".permission-info-button")).toBeVisible();
  const conversationBox = await page.locator(".conversation").boundingBox();
  expect(conversationBox?.x).toBe(0);
  expect(conversationBox?.width).toBe(1280);

  const composer = page.locator(".composer");
  const queuePanel = page.locator(".queue-panel");
  const textarea = page.getByRole("textbox", { name: "发送消息" });
  await expect(queuePanel).toHaveCSS("border-radius", "16px");
  await expect(composer).toHaveCSS("border-radius", "16px");
  await expect(textarea).toHaveCSS("font-size", "14px");
  expect((await textarea.boundingBox())?.height).toBe(44);
  await expect(page.locator(".composer-actions > span")).toBeHidden();

  for (const name of ["引导：补充检查桌面端 icon", "选择截图", "停止", "加入队列"]) {
    const control = page.getByRole("button", { name });
    await expect(control).toHaveText("");
    await expect(control.locator("svg[aria-hidden='true']")).toBeVisible();
  }

  const send = page.getByRole("button", { name: "加入队列" });
  await send.hover();
  await expect.poll(() => send.evaluate(element => getComputedStyle(element, "::after").opacity)).toBe("1");

  const menu = page.getByRole("button", { name: /打开会话列表/ });
  await menu.click();
  const sidebar = page.locator(".sidebar");
  await expect(sidebar).toHaveClass(/\bopen\b/);
  await expect(page.getByRole("button", { name: "关闭会话列表遮罩" })).toBeVisible();
  expect((await sidebar.locator(".thread-list").boundingBox())?.y).toBeLessThanOrEqual(190);
  await expect(sidebar.locator(".sidebar-head, .device-label, .device-meta")).toHaveCount(0);
  expect((await sidebar.locator('[data-thread-id="desktop-unified"]').boundingBox())?.height).toBeLessThanOrEqual(52);
  await expect(page).toHaveScreenshot("desktop-unified-sidebar.png", { animations: "disabled", caret: "hide" });
  await page.getByRole("button", { name: "关闭会话列表遮罩" }).click();
  await expect(sidebar).not.toHaveClass(/\bopen\b/);

  await send.hover();
  await expect(page).toHaveScreenshot("desktop-unified-compact-ui.png", { animations: "disabled", caret: "hide" });
});
