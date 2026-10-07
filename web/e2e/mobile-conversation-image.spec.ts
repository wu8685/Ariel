import { readFileSync } from "node:fs";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"e".repeat(64)}`;
const previewDataUri = `data:image/png;base64,${readFileSync(join(process.cwd(), "public/brand/ariel-logo-wind-messenger-color-512.png")).toString("base64")}`;
const fixtureThread: Thread = {
  threadId: "conversation-image-fixture",
  title: "会话图片预览验收",
  cwd: "/tmp/conversation-image",
  updatedAt: "2026-10-08T00:00:00Z",
  runtime: "idle",
  turns: [{
    turnId: "image-turn",
    status: "completed",
    items: [{
      itemId: "image-message",
      role: "assistant",
      text: "大图会先生成安全预览。\n\n![更新后的架构图](large.png)\n\n失效文件会说明原因。\n\n![已失效图片](missing.png)",
      images: [
        { index: 0, kind: "markdown", alt: "更新后的架构图", source: "large.png" },
        { index: 1, kind: "markdown", alt: "已失效图片", source: "missing.png" },
      ],
    }],
  }],
  pendingInteractions: [],
};

async function openImageFixture(page: Page) {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "conversation-image", relayEpoch: "conversation-image-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "图片测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    if (message.method === "thread.image") {
      if (message.params.imageIndex === 0) {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { dataUri: previewDataUri } }));
      } else {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "rejected", error: { code: "NOT_FOUND", message: "NOT_FOUND" } }));
      }
      return;
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: fixtureThread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread: fixtureThread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 conversation-image，/tmp/conversation-image" }).click();
  await page.getByText(fixtureThread.title).click();
  await expect(page.locator(".sidebar")).not.toHaveClass(/\bopen\b/);
}

test("mobile conversation images open a safe preview and explain missing files", async ({ page }) => {
  await openImageFixture(page);

  await page.getByRole("button", { name: "加载截图：更新后的架构图" }).click();
  const preview = page.getByRole("img", { name: "更新后的架构图" });
  await expect(preview).toBeVisible();
  await page.getByRole("button", { name: "放大截图：更新后的架构图" }).click();
  const dialog = page.getByRole("dialog", { name: "更新后的架构图" });
  await expect(dialog).toBeVisible();
  const geometry = await dialog.locator("img").evaluate(element => {
    const rect = element.getBoundingClientRect();
    return { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom, fit: getComputedStyle(element).objectFit };
  });
  expect(geometry.left).toBeGreaterThanOrEqual(0);
  expect(geometry.top).toBeGreaterThanOrEqual(0);
  expect(geometry.right).toBeLessThanOrEqual(390);
  expect(geometry.bottom).toBeLessThanOrEqual(844);
  expect(geometry.fit).toBe("contain");
  await page.getByRole("button", { name: "关闭截图" }).click();

  await page.getByRole("button", { name: "加载截图：已失效图片" }).click();
  await expect(page.getByText("截图未能加载：原图片文件已不存在。点此重试")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("/Users/");
  await expect(page).toHaveScreenshot("mobile-conversation-image-preview-and-error.png", { animations: "disabled", caret: "hide" });
});
