import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const token = `s_${"f".repeat(64)}`;
const thread: Thread = {
  threadId: "compact-header",
  title: "紧凑会话顶部：很长的中文标题和 long-conversation-title 保持可读",
  cwd: "/work/very-long-workspace-name/another-long-directory-with-documents/ariel",
  updatedAt: "2026-10-26T00:00:00Z",
  runtime: "idle",
  permissions: { sandbox: "full_access", approval: "on_request" },
  turns: [
    {
      turnId: "completed",
      status: "completed",
      items: [
        {
          itemId: "reply",
          role: "assistant",
          text: "正文拥有更多可用空间，字号保持不变。",
        },
      ],
    },
  ],
  pendingInteractions: [],
};

async function openFixture(page: Page) {
  await page.route("**/", async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace(
        "__ARIEL_CHILLING_FILE_CONTEXT__",
        "true",
      ),
    });
  });
  await page.addInitScript(
    (value) => sessionStorage.setItem("ariel.web-session.v1", value),
    token,
  );
  await page.routeWebSocket("**/ws", (socket) =>
    socket.onMessage((raw) => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(
          JSON.stringify({
            type: "hello.ok",
            v: 1,
            connectionId: "compact-header",
            relayEpoch: "relay",
            sessionToken: token,
          }),
        );
        return;
      }
      if (message.type !== "request") return;
      let data: Record<string, unknown> = {};
      if (message.method === "device.list")
        data = {
          devices: [
            {
              deviceId: "mac",
              deviceName: "Header Mac",
              agentOnline: true,
              codexReady: true,
              agentEpoch: "agent",
              capabilities: {
                autoLoad: true,
                send: true,
                chillingFileContext: true,
                chillingWorkspaceBrowser: true,
              },
            },
          ],
        };
      if (message.method === "thread.list")
        data = { threads: [thread], nextCursor: "" };
      if (message.method === "thread.cache.watch")
        data = {
          watchId: "watch",
          freshness: "fresh",
          agentOnline: true,
          agentEpoch: "agent",
          cacheStreamId: "cache",
          seq: 1,
          capturedAt: thread.updatedAt,
          thread,
        };
      if (message.method === "workspace.files.list")
        data = { path: "", revision: "root", entries: [] };
      socket.send(
        JSON.stringify({
          type: "response",
          v: 1,
          requestId: message.requestId,
          outcome: "accepted",
          data,
        }),
      );
    }),
  );
  await page.goto("/");
  await page
    .getByRole("button", { name: `展开项目 ariel，${thread.cwd}` })
    .click();
  await page.getByText(thread.title, { exact: true }).click();
  await expect(page.getByRole("heading", { name: thread.title })).toBeVisible();
}

for (const width of [320, 390, 1280]) {
  test.describe(`viewport ${width}`, () => {
    test.use({ isMobile: width < 800, hasTouch: width < 800 });
    test(`52px header preserves touch targets and working entries at ${width}px`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      await openFixture(page);
      const header = page.locator(".conversation-head");
      await expect(header).toHaveCSS("height", "52px");
      await expect(header.locator("h2")).toHaveCSS("font-size", "14px");
      await expect(header.locator(".head-path")).toHaveCSS("font-size", "10px");
      await expect(page.locator(".message-text")).toHaveCSS(
        "font-size",
        "14px",
      );
      const geometry = await header.evaluate((element) => {
        const bounds = (e: Element) => {
          const { x, y, width, height, right, bottom } =
            e.getBoundingClientRect();
          return { x, y, width, height, right, bottom };
        };
        return {
          header: bounds(element),
          children: [...element.children]
            .filter((e) => getComputedStyle(e).display !== "none")
            .map(bounds),
          controls: [...element.querySelectorAll("button")].map(bounds),
          titleWidth: element.querySelector("h2")!.clientWidth,
          titleScrollWidth: element.querySelector("h2")!.scrollWidth,
          pathWidth: element.querySelector(".head-path")!.clientWidth,
          pathScrollWidth: element.querySelector(".head-path")!.scrollWidth,
          transcriptTop: document
            .querySelector(".transcript")!
            .getBoundingClientRect().top,
          pageWidth: document.documentElement.scrollWidth,
        };
      });
      expect(geometry.header.x).toBe(0);
      expect(geometry.header.width).toBe(width);
      expect(geometry.transcriptTop).toBe(geometry.header.bottom);
      expect(geometry.pageWidth).toBe(width);
      expect(geometry.controls).toHaveLength(2);
      await expect(header.locator(".permission-info-button")).toHaveCount(0);
      const fileEntry = (await header.getByRole("button", { name: "浏览工作区文件" }).boundingBox())!;
      expect(fileEntry.x + fileEntry.width).toBe(width - 10);
      for (const control of geometry.controls) {
        expect(control.width).toBeGreaterThanOrEqual(44);
        expect(control.height).toBeGreaterThanOrEqual(44);
        expect(control.y).toBeGreaterThanOrEqual(geometry.header.y);
        expect(control.bottom).toBeLessThanOrEqual(geometry.header.bottom);
      }
      for (const [index, child] of geometry.children.entries()) {
        expect(child.x).toBeGreaterThanOrEqual(0);
        expect(child.right).toBeLessThanOrEqual(width);
        if (index > 0)
          expect(
            child.x - geometry.children[index - 1].right,
          ).toBeGreaterThanOrEqual(6);
      }
      if (width < 800) {
        expect(geometry.titleScrollWidth).toBeGreaterThan(geometry.titleWidth);
        expect(geometry.pathScrollWidth).toBeGreaterThan(geometry.pathWidth);
      }
      for (const icon of await header.locator("button > svg").all()) {
        await expect(icon).toHaveCSS("width", "18px");
        await expect(icon).toHaveCSS("height", "18px");
      }
      if (width === 390)
        await expect(page).toHaveScreenshot(
          "mobile-compact-conversation-header.png",
          { animations: "disabled", caret: "hide" },
        );

      const menu = header.getByRole("button", { name: /打开会话列表/ });
      await menu.click();
      await expect(page.locator(".sidebar")).toHaveClass(/\bopen\b/);
      // On a 320px screen the drawer covers the backdrop's center. Tap the
      // exposed strip, as a touch user would, rather than forcing the click.
      await page.getByRole("button", { name: "关闭会话列表遮罩" }).click({
        position: { x: width - 4, y: 400 },
      });
      await expect(page.locator(".sidebar")).not.toHaveClass(/\bopen\b/);

      await header.getByRole("button", { name: "浏览工作区文件" }).click();
      await expect(
        page.getByRole("dialog", { name: "工作区文件", exact: true }),
      ).toBeVisible();
      await page.getByRole("button", { name: "返回会话", exact: true }).click();
      const permissions = page.locator(".composer-actions .permission-info-button");
      await page.getByRole("textbox", { name: "发送消息" }).fill("权限弹层测试草稿");
      const attachBox = (await page.getByRole("button", { name: "选择截图" }).boundingBox())!;
      const permissionBox = (await permissions.boundingBox())!;
      expect(permissionBox.x - attachBox.x - attachBox.width).toBeGreaterThanOrEqual(0);
      expect(permissionBox.x - attachBox.x - attachBox.width).toBeLessThanOrEqual(4);
      expect(permissionBox.width).toBeGreaterThanOrEqual(44);
      expect(permissionBox.height).toBeGreaterThanOrEqual(44);
      await permissions.click();
      const popover = page.getByRole("dialog", {
        name: "当前 Desktop 权限详情",
      });
      await expect(popover).toBeVisible();
      const popup = (await popover.boundingBox())!;
      expect(popup.y).toBeGreaterThanOrEqual(12);
      expect(popup.y + popup.height).toBeLessThanOrEqual(permissionBox.y - 7);
      expect(popup.x).toBeGreaterThanOrEqual(0);
      expect(popup.x + popup.width).toBeLessThanOrEqual(width);
      expect(await popover.evaluate(el => { const r=el.getBoundingClientRect(); return el.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)); })).toBe(true);
      if (width === 390) await expect(page).toHaveScreenshot("mobile-composer-permission-popover.png", { animations: "disabled", caret: "hide" });
      await page.setViewportSize({ width, height: 450 });
      await expect(popover).toBeVisible();
      await expect.poll(async () => { const p=(await popover.boundingBox())!, b=(await permissions.boundingBox())!;return p.y>=12 && p.y+p.height<=b.y-7; }).toBe(true);
      expect(await popover.evaluate(el => { const r=el.getBoundingClientRect(); return el.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)); })).toBe(true);
      await permissions.click();
      await expect(popover).toBeHidden();
      await expect(page.getByRole("textbox", { name: "发送消息" })).toHaveValue("权限弹层测试草稿");
      await menu.focus();
      await page.keyboard.press("Enter");
      await expect(page.locator(".sidebar")).toHaveClass(/\bopen\b/);
    });
  });
}
