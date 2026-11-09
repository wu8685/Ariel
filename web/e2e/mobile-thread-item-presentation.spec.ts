import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"6".repeat(64)}`;
const fixtureThread: Thread = {
  threadId: "turn61-item-fixture",
  title: "普通会话条目渲染验收",
  cwd: "/tmp/turn61-item-fixture",
  updatedAt: "2026-11-09T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "turn-61", status: "completed", items: [
    { itemId: "approval", role: "system", text: "已自动审批 3 项\n命令 · 已批准\n文件 · 已批准\n网络 · 已批准" },
    { itemId: "answer", role: "user", text: "> 问题：Choose a fixture color\n\n你的回答：Blue" },
    { itemId: "steered", role: "system", text: "已插入引导" },
    { itemId: "assistant", role: "assistant", text: "Fixture completed." },
    { itemId: "unknown", role: "system", text: "暂不支持显示的内容\n类型：futureTool" },
  ] }],
  pendingInteractions: [],
};

test("ordinary Codex thread safely presents 0.160 private items on mobile and desktop widths", async ({ page }) => {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "turn61", relayEpoch: "turn61-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Fixture Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: fixtureThread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread: fixtureThread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 turn61-item-fixture，/tmp/turn61-item-fixture" }).click();
  await page.getByText(fixtureThread.title).click();

  const approval = page.locator("details.auto-approval");
  const unsupported = page.locator("details.unsupported-item");
  await expect(approval.locator("summary")).toHaveText("已自动审批 3 项");
  await expect(unsupported.locator("summary")).toHaveText("暂不支持显示的内容");
  await expect(page.locator(".steered-notice")).toHaveText("已插入引导");
  await expect(page.locator(".message.user blockquote")).toContainText("问题：Choose a fixture color");
  await expect(page.getByText("你的回答：Blue")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("[automaticApprovalReview 项目]");
  await expect(page.locator("body")).not.toContainText("[steered 项目]");
  await expect(page.locator("body")).not.toContainText("send_user_message_question_reply");
  await expect(page.locator("body")).not.toContainText("questionItemId");
  await expect(approval).not.toHaveAttribute("open", "");
  await expect(page).toHaveScreenshot("mobile-thread-item-presentation.png", { animations: "disabled", caret: "hide" });
  await approval.locator("summary").click();
  await expect(approval).toHaveAttribute("open", "");
  await expect(approval).toContainText("文件 · 已批准");
  await unsupported.locator("summary").click();
  await expect(unsupported).toContainText("类型：futureTool");

  for (const width of [320, 390, 1280]) {
    await page.setViewportSize({ width, height: 844 });
    for (const locator of [approval, unsupported, page.locator(".message.user")]) {
      const box = await locator.boundingBox();
      expect(box).not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
    }
    expect(await page.locator(".conversation").evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  }
});
