import { expect, test, type Page } from "@playwright/test";

const token = `s_${"b".repeat(64)}`;
const assistants = [
  { assistantId: "a-1", name: "小帕", brainDeviceId: "brain-a", status: "ready", lastMessagePreview: "早", lastActivityAt: "2026-11-10T01:57:00Z" },
  { assistantId: "a-2", name: "罗宾", brainDeviceId: "brain-a", status: "ready", lastMessagePreview: "需要你确认方案", lastActivityAt: "2026-11-07T02:00:00Z", unreadCount: 3 },
];
const moves: Array<Record<string, unknown>> = [];

async function openSidebar(page: Page) {
  moves.length = 0;
  let placements: Array<Record<string, unknown>> = [];
  await page.route("**/api/auth/status", route => route.fulfill({ contentType: "application/json", body: JSON.stringify({ mode: "passkey", authenticated: true, enrollmentRequired: false, puck: { state: "ready" } }) }));
  await page.route("http://127.0.0.1:4173/", async route => {
    const response = await route.fetch();
    await route.fulfill({ response, body: (await response.text()).replaceAll("__ARIEL_WEB_AUTH__", "passkey") });
  });
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") { socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "drag", relayEpoch: "drag", sessionToken: token })); return; }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "brain-a", deviceName: "Mac", agentOnline: true, codexReady: true, capabilities: { puckBrain: true, puckBrainReady: true } }] };
    if (message.method === "thread.list") data = { threads: [], nextCursor: "" };
    if (message.method === "puck.list") data = { assistants, maxActiveAssistants: 50 };
    if (message.method === "puck.group.list") data = { groups: [] };
    if (message.method === "puck.history") data = { messages: [] };
    if (message.method === "puck.section.list") data = { sections: [{ sectionId: "section-1", name: "工作机", collapsed: false }], placements, ungroupedCollapsed: false };
    if (message.method === "puck.section.move") {
      moves.push(message.params);
      placements = [{ kind: message.params.kind, entryId: message.params.entryId, sectionId: message.params.sectionId || "", position: 0 }];
      data = { sections: [{ sectionId: "section-1", name: "工作机", collapsed: false }], placements, ungroupedCollapsed: false };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
  }));
  await page.goto("/");
  await page.getByRole("button", { name: "打开 Puck" }).click();
  await expect(page.getByRole("button", { name: "收起工作机" })).toBeVisible();
}

async function center(page: Page, name: string) {
  const box = await page.getByRole("button", { name }).boundingBox();
  if (!box) throw new Error(`missing ${name}`);
  return { x: box.x + box.width / 2, y: box.y + Math.min(24, box.height / 2) };
}

test.describe("Puck sidebar long-press drag", () => {
  test.use({ viewport: { width: 1280, height: 900 }, isMobile: false, hasTouch: true, colorScheme: "dark" });

  test("mouse press-and-hold drags a bot into a section", async ({ page }) => {
    await openSidebar(page);
    const from = await center(page, /罗宾/);
    const to = await center(page, "收起工作机");
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.waitForTimeout(80);
    await page.mouse.move(from.x + 30, from.y);
    await page.waitForTimeout(500);
    await expect(page.locator(".puck-drag-ghost")).toHaveCount(0);
    await page.mouse.up();
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.waitForTimeout(500);
    await expect(page.locator(".puck-drag-ghost")).toHaveText("罗宾");
    await page.mouse.move(to.x, to.y);
    await expect(page.locator(".puck-section.drop-target")).toContainText("工作机");
    await page.mouse.up();
    await expect.poll(() => moves.at(-1)).toMatchObject({ kind: "assistant", entryId: "a-2", sectionId: "section-1" });
    await expect(page.getByRole("button", { name: "收起工作机" }).locator("xpath=ancestor::section[1]")).toContainText("罗宾");
  });

  test("touch long-press drags a bot and a drop outside does not move it", async ({ page }) => {
    await openSidebar(page);
    const from = await center(page, /罗宾/);
    const session = await page.context().newCDPSession(page);
    const touch = async (type: "touchStart" | "touchMove" | "touchEnd", x = 0, y = 0) => {
      await session.send("Input.dispatchTouchEvent", { type, touchPoints: type === "touchEnd" ? [] : [{ x, y, id: 1 }] });
    };
    await touch("touchStart", from.x, from.y);
    await page.waitForTimeout(500);
    await touch("touchMove", 8, 8);
    await touch("touchEnd");
    expect(moves).toHaveLength(0);
    await touch("touchStart", from.x, from.y);
    await page.waitForTimeout(500);
    await expect(page.locator(".puck-drag-ghost")).toHaveText("罗宾");
    await touch("touchMove", from.x, from.y + 4);
    const header = await center(page, "收起工作机");
    await touch("touchMove", header.x, header.y);
    await touch("touchEnd");
    await expect.poll(() => moves.at(-1)).toMatchObject({ kind: "assistant", entryId: "a-2", sectionId: "section-1" });
  });
});
