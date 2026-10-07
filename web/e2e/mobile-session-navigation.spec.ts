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
  await expect(page).toHaveScreenshot("mobile-pinned-sidebar.png", { animations: "disabled", caret: "hide" });
});

test("session loading mark is a blue vector rather than a color emoji", async ({ page }) => {
  await openSessionFixture(page, [regular]);
  await page.getByRole("button", { name: /展开项目 brain-spark/ }).click();
  await page.getByText("普通项目会话").click();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toBeVisible();
  const mark = page.locator(".empty-symbol .session-loading-logo.ariel-logo--micro");
  await expect(mark).toBeVisible();
  await expect(mark).toHaveCSS("color", "rgb(111, 169, 255)");
  await expect(mark).toHaveCSS("width", "50px");
  await expect(mark).toHaveCSS("height", "50px");
  await expect(page.locator(".empty-symbol")).not.toContainText("✳");
  await expect(page).toHaveScreenshot("mobile-session-loading-blue.png", { animations: "disabled", caret: "hide" });
});
