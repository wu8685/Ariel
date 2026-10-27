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

async function dragTouch(context: BrowserContext, page: Page, from: Locator, to: Locator, holdThenCrossText = false) {
  const start = await from.boundingBox();
  const end = await to.boundingBox();
  if (!start || !end) throw new Error("queue drag geometry unavailable");
  const x = start.x + start.width / 2;
  const startY = start.y + start.height / 2;
  const endY = end.y + end.height - 2;
  const endX = holdThenCrossText ? end.x + end.width / 2 : x;
  const cdp = await context.newCDPSession(page);
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x, y: startY, id: 1, radiusX: 4, radiusY: 4, force: 1 }] });
  if (holdThenCrossText) await page.waitForTimeout(650);
  for (let step = 1; step <= 8; step++) {
    await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x: x + (endX - x) * step / 8, y: startY + (endY - startY) * step / 8, id: 1, radiusX: 4, radiusY: 4, force: 1 }] });
  }
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await cdp.detach();
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
  await expect(page.locator(".queue-panel")).toHaveCSS("border-radius", "16px");
  await expect(page.locator(".composer")).toHaveCSS("border-radius", "16px");
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
  await expect(page.locator(".queue-text").first()).toHaveCSS("user-select", "none");
  await dragTouch(context, page, page.getByRole("button", { name: /拖拽排序：第一条/ }), page.locator('[data-queue-id="queue-three"]'), true);
  await expect.poll(() => reorderRequests).toEqual([["queue-two", "queue-three", "queue-one"]]);
  await expect.poll(() => page.locator("[data-queue-id]").evaluateAll(rows => rows.map(row => row.getAttribute("data-queue-id")))).toEqual(["queue-two", "queue-three", "queue-one"]);
  expect(await page.evaluate(() => window.getSelection()?.toString() || "")).toBe("");
  await expect(page).toHaveScreenshot("mobile-queue-reordered.png", { animations: "disabled", caret: "hide" });
});

async function selectTextWithMouse(page: Page, locator: Locator) {
  const box = await locator.boundingBox();
  if (!box) throw new Error("text selection geometry unavailable");
  await page.mouse.move(box.x + 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + Math.min(box.width - 2, 130), box.y + box.height / 2, { steps: 12 });
  await page.mouse.up();
}

for (const width of [320, 390, 1280]) {
  test(`compact queue keeps 44px operation boxes and non-overlapping rows at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    await openQueueFixture(page);
    const boxes = await page.locator(".queue-item").evaluateAll(rows => rows.map(row => {
      const rect = row.getBoundingClientRect();
      return { top: rect.top, bottom: rect.bottom, height: rect.height,
        controls: Array.from(row.querySelectorAll<HTMLButtonElement>(".queue-handle, .queue-actions button")).map(button => {
          const box = button.getBoundingClientRect(); return { top: box.top, bottom: box.bottom, height: box.height };
        }) };
    }));
    expect(boxes).toHaveLength(3);
    boxes.forEach((row, index) => {
      expect(row.height).toBeGreaterThanOrEqual(44);
      expect(row.height).toBeLessThanOrEqual(45);
      row.controls.forEach(control => {
        expect(control.height).toBeGreaterThanOrEqual(44);
        expect(control.top).toBeGreaterThanOrEqual(row.top);
        expect(control.bottom).toBeLessThanOrEqual(row.bottom);
      });
      if (index) expect(row.top).toBeGreaterThanOrEqual(boxes[index - 1].bottom);
    });
    expect(await page.evaluate(() => document.body.scrollWidth <= innerWidth)).toBe(true);
    await expect(page).toHaveScreenshot(`compact-queue-${width}.png`, { animations: "disabled", caret: "hide" });
  });
}

for (const width of [390, 1280]) {
  test.describe(`queue selection at ${width}px`, () => {
    test.use({ viewport: { width, height: 844 }, isMobile: false, hasTouch: false, deviceScaleFactor: 1 });

    test("queue display text is not selectable while transcript and queue editing remain selectable", async ({ page }) => {
      const reorderRequests = await openQueueFixture(page);
      await selectTextWithMouse(page, page.locator(".queue-text").first());
      expect(await page.evaluate(() => window.getSelection()?.toString() || "")).toBe("");
      await expect(page.locator(".queue-text").first()).toHaveCSS("user-select", "none");
      expect(reorderRequests).toHaveLength(0);

      await selectTextWithMouse(page, page.locator(".message-text").first());
      expect(await page.evaluate(() => window.getSelection()?.toString() || "")).not.toBe("");

      await page.getByRole("button", { name: /更多选项：第一条/ }).click();
      await page.getByRole("menuitem", { name: "编辑", exact: true }).click();
      const input = page.getByRole("textbox", { name: "发送消息" });
      await expect(input).toHaveValue(initialQueue[0].text);
      await input.press("ControlOrMeta+a");
      await input.press("ArrowLeft");
      for (let i = 0; i < 3; i++) await input.press("Shift+ArrowRight");
      expect(await input.evaluate((element: HTMLTextAreaElement) => [element.selectionStart, element.selectionEnd])).toEqual([0, 3]);
      await input.pressSequentially("修改后");
      await expect(input).toHaveValue(`修改后${initialQueue[0].text.slice(3)}`);
      await expect(page.getByRole("button", { name: "保存排队消息", exact: true })).toBeEnabled();
    });
  });
}
