import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"d".repeat(64)}`;
const shortNotice = "暂时无法加载，请稍后重试。";
const longNotice = "暂时无法读取更早的会话内容，当前正文和草稿已保留。请检查电脑上的 Desktop Agent 连接状态，确认网络恢复后再尝试；不要重复提交结果不确定的消息。";
const thread: Thread = {
  threadId: "compact-notice", title: "告警条留白验收", cwd: "/tmp/compact-notice",
  updatedAt: "2026-10-27T00:00:00Z", runtime: "idle", historyComplete: false, recentComplete: true,
  turns: [{ turnId: "latest", status: "completed", items: [{ itemId: "reply", role: "assistant", text: "当前会话内容保持可读。" }] }],
  pendingInteractions: [],
};

async function fixture(page: Page) {
  const methods: string[] = [];
  let historyReads = 0;
  await page.addInitScript(value => sessionStorage.setItem("ariel.web-session.v1", value), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "notice", relayEpoch: "relay-notice", sessionToken })); return;
    }
    if (message.type !== "request") return;
    methods.push(message.method);
    if (message.method === "thread.history") {
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "rejected", error: { code: "NOT_FOUND", message: ++historyReads === 1 ? shortNotice : longNotice } })); return;
    }
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Notice Mac", agentEpoch: "epoch", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, history: true, send: true } }] };
    if (message.method === "thread.list") data = { threads: [thread], nextCursor: "" };
    if (message.method === "thread.cache.watch") data = { watchId: "watch", freshness: "fresh", agentOnline: true, agentEpoch: "epoch", cacheStreamId: "stream", seq: 1, capturedAt: "2026-10-27T00:00:00Z", thread };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
  }));
  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 compact-notice，/tmp/compact-notice" }).click();
  await page.getByText(thread.title, { exact: true }).click();
  await expect(page.getByText("当前会话内容保持可读。")).toBeVisible();
  return methods;
}

for (const width of [320, 390, 1280]) {
  test(`composer notice stays compact and dismissible at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    const methods = await fixture(page);
    const draft = page.getByRole("textbox", { name: "发送消息" });
    await draft.fill("关闭提示不能丢掉这份草稿");
    const notice = page.locator(".composer-stack > .notice");
    const close = page.getByRole("button", { name: "关闭提示", exact: true });

    async function assertGeometry() {
      const bar = await notice.boundingBox(), target = await close.boundingBox(), composer = await page.locator(".composer").boundingBox();
      expect(bar && target && composer).toBeTruthy();
      expect(target!.width).toBe(44); expect(target!.height).toBe(44);
      expect(target!.y).toBeGreaterThanOrEqual(bar!.y); expect(target!.y + target!.height).toBeLessThanOrEqual(bar!.y + bar!.height);
      expect(target!.x).toBeGreaterThanOrEqual(bar!.x); expect(target!.x + target!.width).toBeLessThanOrEqual(bar!.x + bar!.width);
      expect(bar!.y + bar!.height).toBeLessThanOrEqual(composer!.y);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
      await expect(notice).toHaveCSS("font-size", "12px");
      await expect(draft).toHaveValue("关闭提示不能丢掉这份草稿");
      return bar!;
    }

    await page.getByRole("button", { name: "加载更早消息", exact: true }).click();
    await expect(notice).toContainText(shortNotice);
    const short = await assertGeometry();
    expect(short.height).toBe(50);
    await expect(page).toHaveScreenshot(`compact-notice-short-${width}.png`, { animations: "disabled", caret: "hide" });
    // Transparent edge outside the visible close glyph remains a real target.
    await close.click({ position: { x: 2, y: 22 } });
    await expect(notice).toHaveCount(0);
    await expect(draft).toHaveValue("关闭提示不能丢掉这份草稿");

    await page.getByRole("button", { name: "加载更早消息", exact: true }).click();
    await expect(notice).toContainText(longNotice);
    const long = await assertGeometry();
    expect(long.height).toBeGreaterThanOrEqual(short.height);
    if (width === 320) expect(long.height).toBeGreaterThan(short.height);
    await expect(page).toHaveScreenshot(`compact-notice-long-${width}.png`, { animations: "disabled", caret: "hide" });
    await close.focus(); await close.press("Enter");
    await expect(notice).toHaveCount(0);
    await expect(draft).toHaveValue("关闭提示不能丢掉这份草稿");
    expect(methods.filter(method => method === "thread.history")).toHaveLength(2);
    expect(methods.some(method => method.startsWith("turn.") || method.startsWith("queue."))).toBe(false);
  });
}
