import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"b".repeat(64)}`;
const thread: Thread = {
  threadId: "send-confirmation",
  title: "发送确认回归",
  cwd: "/tmp/send-confirmation",
  updatedAt: "2026-10-11T00:00:00Z",
  runtime: "idle",
  turns: [],
  pendingInteractions: [],
};

test("an accepted send clears after a same-thread resync on a phone viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let relaySocket: { send(data: string): void } | null = null;
  let sendRequest: Record<string, any> | null = null;
  let subscription = 0;
  await page.routeWebSocket("**/ws", socket => {
    relaySocket = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "send", relayEpoch: "relay", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "device.list") {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { devices: [{ deviceId: "mac", deviceName: "Send Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true, send: true } }] } }));
      } else if (message.method === "thread.list") {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { threads: [thread], nextCursor: "" } }));
      } else if (message.method === "thread.subscribe") {
        subscription++;
        const sub = `sub-${subscription}`;
        const stream = `stream-${subscription}`;
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { subscriptionId: sub } }));
        socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: sub, streamId: stream, seq: 1, thread }));
      } else if (message.method === "turn.start") {
        sendRequest = message;
      }
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 send-confirmation，/tmp/send-confirmation" }).click();
  await page.getByText(thread.title).click();
  const composer = page.getByLabel("发送消息");
  await composer.fill("重同步期间仍应确认清空");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect.poll(() => sendRequest?.method).toBe("turn.start");

  relaySocket!.send(JSON.stringify({ type: "event", v: 1, event: "thread.error", deviceId: "mac", threadId: thread.threadId, subscriptionId: "sub-1", streamId: "stream-1", code: "RESYNC_REQUIRED" }));
  await expect.poll(() => subscription).toBe(2);
  relaySocket!.send(JSON.stringify({ type: "response", v: 1, requestId: sendRequest!.requestId, outcome: "accepted", data: { turnId: "confirmed" } }));

  await expect(composer).toHaveValue("");
  expect(sendRequest!.params.clientMessageId).toMatch(/^[0-9a-f-]{36}$/);
});
