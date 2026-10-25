import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"f".repeat(64)}`;
const revision = "a".repeat(64);
const fixtureThread: Thread = {
  threadId: "tool-lazy-fixture",
  title: "工具详情按需读取验收",
  cwd: "/tmp/tool-lazy",
  updatedAt: "2026-10-25T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "turn-tool", status: "completed", items: [{
    itemId: "tool-item", role: "system", text: "工具调用: fixture/lookup",
    activity: { kind: "mcpToolCall", label: "fixture/lookup", status: "completed", detailState: "available", detailRevision: revision, truncated: false },
  }] }],
  pendingInteractions: [],
};

test("mobile tool detail stays lazy until the selected item is opened", async ({ page }) => {
  let detailReads = 0;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "tool-lazy", relayEpoch: "tool-lazy-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "工具测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    if (message.method === "thread.activity.read") {
      detailReads += 1;
      data = { text: "参数: query=fixture\n输出: done", nextCursor: "", revision, complete: true };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: fixtureThread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread: fixtureThread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 tool-lazy，/tmp/tool-lazy" }).click();
  await page.getByText(fixtureThread.title).click();
  await expect(page.getByLabel("最近工具摘要")).toHaveText("fixture/lookup（已完成）");
  expect(detailReads).toBe(0);
  await page.getByRole("button", { name: /打开工具调用.*1 项/ }).click();
  expect(detailReads).toBe(0);
  await expect(page.getByText("fixture/lookup")).toBeVisible();
  await expect(page.getByText("参数: query=fixture", { exact: false })).toHaveCount(0);

  const detailButton = page.getByRole("button", { name: "查看详情" });
  await detailButton.click();
  await expect(page.getByText("参数: query=fixture", { exact: false })).toBeVisible();
  expect(detailReads).toBe(1);
  const box = await page.getByRole("button", { name: "收起详情" }).boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390);
});
