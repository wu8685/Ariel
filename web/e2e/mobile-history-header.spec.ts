import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"c".repeat(64)}`;
const fixtureThread: Thread = {
  threadId: "history-fixture",
  title: "阅读历史时收起顶部",
  cwd: "/tmp/history-fixture",
  updatedAt: "2026-10-07T00:00:00Z",
  runtime: "idle",
  turns: Array.from({ length: 18 }, (_, index) => ({
    turnId: `turn-${index}`,
    status: "completed",
    items: [{
      itemId: `item-${index}`,
      role: index % 2 === 0 ? "user" : "assistant",
      text: `第 ${index + 1} 段对话历史。${"这是一段用于验证移动端滚动空间和标题栏折叠行为的内容。".repeat(3)}`,
    }],
  })),
  pendingInteractions: [],
};

async function openHistoryFixture(page: Page) {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "history", relayEpoch: "history-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "历史测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: fixtureThread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread: fixtureThread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 history-fixture，/tmp/history-fixture" }).click();
  await page.getByText(fixtureThread.title).click();
  await expect(page.getByRole("heading", { name: fixtureThread.title })).toBeVisible();
  await expect(page.locator(".sidebar")).not.toHaveClass(/\bopen\b/);
}

test("scrolling into history fully collapses the chrome and gives its height to the transcript", async ({ page }) => {
  await openHistoryFixture(page);
  const head = page.locator(".conversation-head");
  const transcript = page.locator(".transcript");
  const composerWrap = page.locator(".composer-wrap");
  const initial = await page.evaluate(() => ({
    headHeight: document.querySelector(".conversation-head")?.getBoundingClientRect().height || 0,
    transcriptTop: document.querySelector(".transcript")?.getBoundingClientRect().top || 0,
    transcriptHeight: document.querySelector(".transcript")?.getBoundingClientRect().height || 0,
    composerHeight: document.querySelector(".composer-wrap")?.getBoundingClientRect().height || 0,
  }));
  expect(initial.headHeight).toBe(66);

  await transcript.evaluate(element => {
    element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight - 100);
    element.dispatchEvent(new Event("scroll"));
  });
  await expect(head).toHaveAttribute("data-history-collapsed", "true");
  await expect(head).toHaveCSS("height", "0px");
  await expect(head).toHaveCSS("visibility", "hidden");
  await expect(head.getByRole("button", { name: /打开会话列表/ })).toBeHidden();
  await expect(head.locator(".permission-info-button")).toBeHidden();
  await expect(head.getByRole("heading", { name: fixtureThread.title })).toBeHidden();
  await expect(head.locator(".head-path")).toBeHidden();
  await expect(composerWrap).toHaveAttribute("data-history-collapsed", "true");
  await expect(composerWrap).toHaveCSS("height", "0px");
  await expect(composerWrap).toHaveCSS("visibility", "hidden");
  await expect(page.getByRole("textbox", { name: "发送消息" })).toBeHidden();
  const collapsed = await page.evaluate(() => ({
    headHeight: document.querySelector(".conversation-head")?.getBoundingClientRect().height || 0,
    transcriptTop: document.querySelector(".transcript")?.getBoundingClientRect().top || 0,
    transcriptHeight: document.querySelector(".transcript")?.getBoundingClientRect().height || 0,
  }));
  expect(collapsed.headHeight).toBe(0);
  expect(initial.transcriptTop - collapsed.transcriptTop).toBeGreaterThanOrEqual(66);
  expect(collapsed.transcriptHeight - initial.transcriptHeight).toBeGreaterThanOrEqual(initial.headHeight + initial.composerHeight);
  const restore = page.getByRole("button", { name: "恢复输入区并回到最新" });
  await expect(restore).toBeVisible();
  await expect(restore.locator("..")).toHaveCSS("position", "absolute");
  await expect(page).toHaveScreenshot("mobile-history-header-collapsed.png", { animations: "disabled", caret: "hide" });

  await restore.click();
  await expect(head).toHaveAttribute("data-history-collapsed", "false");
  await expect(head).toHaveCSS("height", "66px");
  await expect(head).toHaveCSS("visibility", "visible");
  await expect(head.locator(".head-path")).toBeVisible();
  await expect(composerWrap).toHaveAttribute("data-history-collapsed", "false");
  await expect(page.getByRole("textbox", { name: "发送消息" })).toBeVisible();
});
