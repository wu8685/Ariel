import { expect, test, type BrowserContext, type Locator, type Page } from "@playwright/test";

const sessionToken = `s_${"a".repeat(64)}`;
const initialQueue = [
  { queueId: "queue-one", clientMessageId: "message-one", text: "第一条：检查移动端输入框布局", images: [] as string[], editable: true },
  { queueId: "queue-two", clientMessageId: "message-two", text: "第二条：继续验证 streaming 稳定性", images: [] as string[], editable: true },
  { queueId: "queue-three", clientMessageId: "message-three", text: "第三条：整理最终测试结论", images: [] as string[], editable: true },
];
const fixtureThread = {
  threadId: "fixture",
  title: "UI 视觉验收 fixture",
  cwd: "/tmp/fixture",
  updatedAt: "2026-10-07T00:00:00Z",
  runtime: "inProgress",
  turns: [{ turnId: "active-turn", status: "inProgress", items: [{ itemId: "assistant", role: "assistant", text: "正在执行视觉验收。", images: [] }] }],
  pendingInteractions: [],
  queuedMessages: initialQueue,
};

async function openQueueFixture(page: Page): Promise<string[][]> {
  const reorderRequests: string[][] = [];
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "visual", relayEpoch: "visual-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "视觉测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, queue: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    if (message.method === "queue.reorder") {
      reorderRequests.push(message.params.queueIds);
      const byID = new Map(initialQueue.map(item => [item.queueId, item]));
      data = { queuedMessages: message.params.queueIds.map((id: string) => byID.get(id)) };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: "fixture", subscriptionId: "sub", streamId: "stream", seq: 1, thread: fixtureThread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 fixture，/tmp/fixture" }).click();
  await page.getByText("UI 视觉验收 fixture").click();
  await page.getByLabel("排队的后续输入").waitFor();
  await expect(page.locator(".sidebar")).not.toHaveClass(/\bopen\b/);
  await page.waitForTimeout(250);
  return reorderRequests;
}

async function dragTouch(context: BrowserContext, page: Page, from: Locator, to: Locator) {
  const start = await from.boundingBox();
  const end = await to.boundingBox();
  if (!start || !end) throw new Error("queue drag geometry unavailable");
  const x = start.x + start.width / 2;
  const startY = start.y + start.height / 2;
  const endY = end.y + end.height - 2;
  const cdp = await context.newCDPSession(page);
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x, y: startY, id: 1, radiusX: 4, radiusY: 4, force: 1 }] });
  for (let step = 1; step <= 8; step++) {
    await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x, y: startY + (endY - startY) * step / 8, id: 1, radiusX: 4, radiusY: 4, force: 1 }] });
  }
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
}

test("mobile queue and overlay remain clear above the composer", async ({ page }) => {
  await openQueueFixture(page);
  const geometry = await page.evaluate(() => ({
    viewportWidth: window.innerWidth,
    bodyWidth: document.body.scrollWidth,
    queueBottom: document.querySelector(".queue-panel")?.getBoundingClientRect().bottom || 0,
    composerTop: document.querySelector(".composer")?.getBoundingClientRect().top || 0,
  }));
  expect(geometry.bodyWidth).toBeLessThanOrEqual(geometry.viewportWidth);
  expect(geometry.queueBottom).toBeLessThanOrEqual(geometry.composerTop);
  await expect(page.locator(".queue-text").first()).toHaveCSS("font-size", "12px");
  await expect(page.locator(".composer textarea")).toHaveCSS("font-size", "14px");

  const moreButton = page.getByRole("button", { name: /更多选项：第二条/ });
  await moreButton.click();
  const menu = page.getByRole("menu", { name: /排队消息选项：第二条/ });
  await expect(menu).toBeVisible();
  const editItem = menu.getByRole("menuitem", { name: "编辑" });
  await expect(menu).toHaveCSS("font-size", "12px");
  await expect(editItem).toHaveCSS("font-size", "12px");
  await expect(editItem).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  const menuBox = await menu.boundingBox();
  const editItemBox = await editItem.boundingBox();
  const moreButtonBox = await moreButton.boundingBox();
  const menuRight = (menuBox?.x || 0) + (menuBox?.width || 0);
  const moreButtonRight = (moreButtonBox?.x || 0) + (moreButtonBox?.width || 0);
  expect(menuBox?.width || 0).toBeLessThanOrEqual(108);
  expect(editItemBox?.height || 0).toBeGreaterThanOrEqual(34);
  expect(Math.abs(menuRight - moreButtonRight)).toBeLessThanOrEqual(1);
  expect(menuBox?.y || 0).toBeGreaterThanOrEqual(0);
  expect((menuBox?.y || 0) + (menuBox?.height || 0)).toBeLessThanOrEqual(geometry.composerTop);
  await expect(page).toHaveScreenshot("mobile-queue-menu.png", { animations: "disabled", caret: "hide" });
});

test("touch drag moves the first queued item to the end", async ({ context, page }) => {
  const reorderRequests = await openQueueFixture(page);
  await dragTouch(context, page, page.getByRole("button", { name: /拖拽排序：第一条/ }), page.locator('[data-queue-id="queue-three"]'));
  await expect.poll(() => reorderRequests).toEqual([["queue-two", "queue-three", "queue-one"]]);
  await expect.poll(() => page.locator("[data-queue-id]").evaluateAll(rows => rows.map(row => row.getAttribute("data-queue-id")))).toEqual(["queue-two", "queue-three", "queue-one"]);
  await expect(page).toHaveScreenshot("mobile-queue-reordered.png", { animations: "disabled", caret: "hide" });
});
