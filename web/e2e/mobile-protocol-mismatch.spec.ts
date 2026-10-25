import { expect, test, type WebSocketRoute } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"c".repeat(64)}`;
const thread: Thread = {
  threadId: "protocol-mismatch",
  title: "协议兼容性回归",
  cwd: "/tmp/protocol-mismatch",
  updatedAt: "2026-10-26T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "confirmed", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "浏览器已经确认的正文" }] }],
  pendingInteractions: [],
};

test("keeps confirmed content and stops reconnecting when Relay reports a protocol mismatch", async ({ page }) => {
  let connections = 0;
  let relaySocket: WebSocketRoute | null = null;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    connections++;
    relaySocket = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "protocol", relayEpoch: "relay", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "thread.cache.watch") {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "rejected", error: { code: "PROTOCOL_UNSUPPORTED", message: "PROTOCOL_UNSUPPORTED" } }));
        return;
      }
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "兼容性测试 Mac", agentOnline: true, codexReady: true, agentEpoch: "agent", capabilities: { autoLoad: true } }] };
      if (message.method === "thread.list") data = { threads: [thread], nextCursor: "" };
      if (message.method === "thread.read") data = { thread };
      if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
      if (message.method === "thread.subscribe") {
        socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread }));
      }
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 protocol-mismatch，/tmp/protocol-mismatch" }).click();
  await page.getByText(thread.title).click();
  await expect(page.getByText("浏览器已经确认的正文")).toBeVisible();

  const connectionsBeforeMismatch = connections;
  relaySocket!.close({ code: 1008, reason: "invalid message" });
  await expect(page.getByText("浏览器已经确认的正文")).toBeVisible();
  await expect(page.getByText("页面版本与 Relay 不兼容，操作已暂停。请刷新页面。")).toBeVisible();
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  await expect(page.getByLabel("连接 Relay")).toHaveCount(0);
  await page.waitForTimeout(1_500);
  expect(connections).toBe(connectionsBeforeMismatch);
});
