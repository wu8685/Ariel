import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"c".repeat(64)}`;
const thread: Thread = {
  threadId: "00000000-0000-4000-8000-000000000068",
  title: "可归档会话",
  cwd: "/tmp/archive-fixture",
  updatedAt: "2026-10-11T00:00:00Z",
  runtime: "idle",
  turns: [],
  pendingInteractions: [],
};

test("a stationary phone long press archives a verified idle thread and offers undo", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let archiveCount = 0;
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "archive", relayEpoch: "relay", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Archive Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true, threadArchive: true, threadArchiveUndo: true } }] };
    if (message.method === "thread.list") data = { threads: [thread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    if (message.method === "thread.archive.update") { archiveCount++; data = { operationId: "archive-operation" }; }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 archive-fixture，/tmp/archive-fixture" }).click();
  await page.getByText(thread.title).click();
  await page.getByRole("button", { name: /打开会话列表/ }).click();
  const row = page.locator(`.thread-row[data-thread-id="${thread.threadId}"]`);
  await row.dispatchEvent("pointerdown", { pointerId: 4, pointerType: "touch", button: 0, clientX: 120, clientY: 260 });
  await page.waitForTimeout(550);
  await expect(page.getByRole("menuitem", { name: "归档" })).toBeVisible();
  await page.getByRole("menuitem", { name: "归档" }).click();

  await expect.poll(() => archiveCount).toBe(1);
  await expect(page.getByRole("button", { name: "撤销归档" })).toBeVisible();
  await expect(row).toHaveCount(0);
});
