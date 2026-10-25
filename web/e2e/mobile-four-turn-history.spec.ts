import { expect, test } from "@playwright/test";
import type { Thread, Turn } from "../src/generated/protocol";

const sessionToken = `s_${"f".repeat(64)}`;
const turn = (id: string): Turn => ({ turnId: id, status: "completed", items: [{ itemId: `${id}-item`, role: "assistant", text: id }] });
const recent: Thread = {
  threadId: "two-turn-window",
  title: "双回合首屏",
  cwd: "/tmp/two-turn-window",
  updatedAt: "2026-10-11T00:00:00Z",
  runtime: "idle",
  historyComplete: false,
  recentComplete: false,
  turns: [turn("turn-4")],
  pendingInteractions: [],
};

test("fills only two recent turns while manual history uses three-turn pages", async ({ page }) => {
  const historyLimits: number[] = [];
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "four", relayEpoch: "relay-four", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Four Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-four", capabilities: { autoLoad: true, history: true } }] };
    if (message.method === "thread.list") data = { threads: [recent], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub-four" };
    if (message.method === "thread.history") {
      historyLimits.push(message.params.limit);
      data = historyLimits.length === 1
        ? { turns: [turn("turn-4"), turn("turn-3")], nextCursor: "older" }
        : { turns: [turn("turn-0")], nextCursor: "" };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: recent.threadId, subscriptionId: "sub-four", streamId: "stream-four", seq: 1, thread: recent }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 two-turn-window，/tmp/two-turn-window" }).click();
  await page.getByText(recent.title).click();
  await expect.poll(() => historyLimits).toEqual([1]);
  await expect(page.getByText("turn-3")).toBeVisible();
  await expect(page.getByText("turn-4")).toBeVisible();

  await page.getByRole("button", { name: "加载更早消息" }).click();
  await expect.poll(() => historyLimits).toEqual([1, 3]);
  await expect(page.getByText("turn-0")).toBeVisible();
});
