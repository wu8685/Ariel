import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

test.use({ reducedMotion: "no-preference" });

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

async function openHistoryFixture(page: Page, thread: Thread = fixtureThread, fakeVisualViewport = false) {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  if (fakeVisualViewport) await page.addInitScript(() => {
    const viewport = Object.assign(new EventTarget(), { height: 844, offsetTop: 0 });
    Object.defineProperty(window, "visualViewport", { configurable: true, value: viewport });
    (window as unknown as { __resizeArielViewport: (height: number) => void }).__resizeArielViewport = height => {
      viewport.height = height;
      viewport.dispatchEvent(new Event("resize"));
    };
  });
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "history", relayEpoch: "history-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "历史测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [thread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 history-fixture，/tmp/history-fixture" }).click();
  await page.getByText(thread.title).click();
  await expect(page.getByRole("heading", { name: thread.title })).toBeVisible();
  await expect(page.locator(".sidebar")).not.toHaveClass(/\bopen\b/);
}

test("the running composer stays visible when the mobile keyboard shrinks the viewport", async ({ page }) => {
  const running: Thread = {
    ...fixtureThread,
    runtime: "inProgress",
    turns: fixtureThread.turns.map((turn, index) => index === fixtureThread.turns.length - 1 ? { ...turn, status: "inProgress" } : turn),
  };
  await openHistoryFixture(page, running, true);
  const input = page.getByRole("textbox", { name: "发送消息" });
  const composerWrap = page.locator(".composer-wrap");

  await input.focus();
  await input.fill("处理中也要保留的草稿");
  await page.evaluate(() => {
    (window as unknown as { __resizeArielViewport: (height: number) => void }).__resizeArielViewport(500);
    document.querySelector(".transcript")?.dispatchEvent(new Event("scroll"));
  });

  await expect(composerWrap).toHaveAttribute("data-history-collapsed", "false");
  await expect(input).toBeVisible();
  await expect(input).toBeFocused();
  await expect(input).toHaveValue("处理中也要保留的草稿");
  const geometry = await page.evaluate(() => ({
    shellHeight: document.querySelector(".app-shell")?.getBoundingClientRect().height || 0,
    composerBottom: document.querySelector(".composer-wrap")?.getBoundingClientRect().bottom || 0,
  }));
  expect(geometry.shellHeight).toBe(500);
  expect(geometry.composerBottom).toBeLessThanOrEqual(500);
});

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
  await expect(restore).toHaveText("");
  await expect(restore.locator("svg")).toBeVisible();
  const restoreBox = await restore.boundingBox();
  expect(restoreBox?.width).toBe(40);
  expect(restoreBox?.height).toBe(40);
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

test("returning to latest restores the chrome once without oscillation", async ({ page }) => {
  await openHistoryFixture(page);
  const head = page.locator(".conversation-head");
  const transcript = page.locator(".transcript");
  const composerWrap = page.locator(".composer-wrap");

  await transcript.evaluate(element => {
    element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight - 500);
    element.dispatchEvent(new Event("scroll"));
  });
  await expect(head).toHaveAttribute("data-history-collapsed", "true");
  await expect(head).toHaveCSS("height", "0px");

  await page.evaluate(() => {
    const head = document.querySelector(".conversation-head");
    const states: string[] = [];
    (window as unknown as { __arielHistoryChromeStates: string[] }).__arielHistoryChromeStates = states;
    if (!head) return;
    new MutationObserver(() => states.push(head.getAttribute("data-history-collapsed") || "")).observe(head, { attributes: true, attributeFilter: ["data-history-collapsed"] });
  });
  await transcript.evaluate(element => {
    element.scrollTop = element.scrollHeight;
    element.dispatchEvent(new Event("scroll"));
  });

  await expect(head).toHaveAttribute("data-history-collapsed", "false");
  await page.waitForTimeout(550);
  const settled = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript") as HTMLElement;
    return {
      states: (window as unknown as { __arielHistoryChromeStates: string[] }).__arielHistoryChromeStates,
      distanceFromLatest: transcript.scrollHeight - transcript.clientHeight - transcript.scrollTop,
      headHeight: document.querySelector(".conversation-head")?.getBoundingClientRect().height || 0,
      composerHeight: document.querySelector(".composer-wrap")?.getBoundingClientRect().height || 0,
    };
  });
  expect(settled.states).toEqual(["false"]);
  expect(settled.distanceFromLatest).toBeLessThanOrEqual(1);
  expect(settled.headHeight).toBe(66);
  expect(settled.composerHeight).toBeGreaterThan(0);
  await expect(composerWrap).toHaveAttribute("data-history-collapsed", "false");
  await expect(page).toHaveScreenshot("mobile-history-latest-stable.png", { animations: "disabled", caret: "hide" });
});

test("a one-pixel final step to the latest restores the chrome", async ({ page }) => {
  await openHistoryFixture(page);
  const head = page.locator(".conversation-head");
  const transcript = page.locator(".transcript");
  const composerWrap = page.locator(".composer-wrap");

  await transcript.evaluate(element => {
    element.scrollTop = Math.max(0, element.scrollHeight - element.clientHeight - 500);
    element.dispatchEvent(new Event("scroll"));
  });
  await expect(head).toHaveAttribute("data-history-collapsed", "true");

  await page.evaluate(() => {
    const head = document.querySelector(".conversation-head");
    const states: string[] = [];
    (window as unknown as { __arielHistoryChromeStates: string[] }).__arielHistoryChromeStates = states;
    if (!head) return;
    new MutationObserver(() => states.push(head.getAttribute("data-history-collapsed") || "")).observe(head, { attributes: true, attributeFilter: ["data-history-collapsed"] });
  });
  await transcript.evaluate(element => {
    const bottom = element.scrollHeight - element.clientHeight;
    element.scrollTop = bottom - 33;
    element.dispatchEvent(new Event("scroll"));
    element.scrollTop = bottom - 32;
    element.dispatchEvent(new Event("scroll"));
  });

  await expect(head).toHaveAttribute("data-history-collapsed", "false");
  await page.waitForTimeout(550);
  const settled = await page.evaluate(() => {
    const transcript = document.querySelector(".transcript") as HTMLElement;
    return {
      states: (window as unknown as { __arielHistoryChromeStates: string[] }).__arielHistoryChromeStates,
      distanceFromLatest: transcript.scrollHeight - transcript.clientHeight - transcript.scrollTop,
      headHeight: document.querySelector(".conversation-head")?.getBoundingClientRect().height || 0,
      composerHeight: document.querySelector(".composer-wrap")?.getBoundingClientRect().height || 0,
    };
  });
  expect(settled.states).toEqual(["false"]);
  expect(settled.distanceFromLatest).toBeLessThanOrEqual(1);
  expect(settled.headHeight).toBe(66);
  expect(settled.composerHeight).toBeGreaterThan(0);
  await expect(composerWrap).toHaveAttribute("data-history-collapsed", "false");
  await expect(page).toHaveScreenshot("mobile-history-manual-latest-restored.png", { animations: "disabled", caret: "hide" });
});
