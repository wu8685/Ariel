import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"d".repeat(64)}`;
const thread: Thread = {
  threadId: "incremental-thread",
  title: "增量同步会话",
  cwd: "/tmp/incremental-sync",
  updatedAt: "2026-10-11T00:00:00Z",
  runtime: "inProgress",
  turns: [
    { turnId: "stable", status: "completed", items: [{ itemId: "stable-item", role: "assistant", text: "不会变化的历史" }] },
    { turnId: "active", status: "inProgress", items: [{ itemId: "active-item", role: "assistant", text: "A" }] },
  ],
  pendingInteractions: [],
};

test("applies item deltas without replacing stable DOM and keeps cached content visible during resync", async ({ page }) => {
  let relaySocket: { send(data: string): void } | null = null;
  let subscriptions = 0;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    relaySocket = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "incremental", relayEpoch: "relay", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Incremental Mac", agentOnline: true, codexReady: true, agentEpoch: "agent-incremental", capabilities: { autoLoad: true } }] };
      if (message.method === "thread.list") data = { threads: [thread], nextCursor: "" };
      if (message.method === "thread.subscribe") {
        subscriptions++;
        data = { subscriptionId: `sub-${subscriptions}` };
        expect(message.params.updateMode).toBe("item-delta-v1");
      }
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
      if (message.method === "thread.subscribe") {
        const subscription = subscriptions;
        const snapshot = { type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: `sub-${subscription}`, streamId: `stream-${subscription}`, seq: 1, thread };
        if (subscription === 1) socket.send(JSON.stringify(snapshot));
        else setTimeout(() => socket.send(JSON.stringify(snapshot)), 300);
      }
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 incremental-sync，/tmp/incremental-sync" }).click();
  await page.getByText(thread.title).click();
  await expect(page.getByText("不会变化的历史")).toBeVisible();
  await expect(page.getByText("A", { exact: true })).toBeVisible();

  const stable = page.locator('[data-item-id="stable-item"]');
  await stable.evaluate(element => { element.setAttribute("data-dom-identity", "preserved"); });
  const composer = page.getByLabel("发送消息");
  await composer.fill("streaming 期间保留的草稿");
  await composer.focus();

  relaySocket!.send(JSON.stringify({
    type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: thread.threadId,
    subscriptionId: "sub-1", streamId: "stream-1", baseSeq: 1, seq: 2,
    patch: {
      threadState: { threadId: thread.threadId, title: thread.title, cwd: thread.cwd, updatedAt: "2026-10-11T00:00:01Z", runtime: "inProgress", pendingInteractions: [], queuedMessages: [] },
      turnOrder: ["stable", "active"],
      turnPatches: [{ turnId: "active", status: "inProgress", itemOrder: ["active-item"], upsertItems: [{ itemId: "active-item", role: "assistant", text: "AB" }], removedItemIds: [] }],
      removedTurnIds: [],
    },
  }));
  await expect(page.getByText("AB", { exact: true })).toBeVisible();
  await expect(stable).toHaveAttribute("data-dom-identity", "preserved");
  await expect(composer).toHaveValue("streaming 期间保留的草稿");
  await expect(composer).toBeFocused();

  relaySocket!.send(JSON.stringify({
    type: "event", v: 1, event: "thread.update", deviceId: "mac", threadId: thread.threadId,
    subscriptionId: "sub-1", streamId: "stream-1", baseSeq: 2, seq: 4, thread,
  }));
  await expect.poll(() => subscriptions).toBe(2);
  await expect(page.getByText("AB", { exact: true })).toBeVisible();
  await expect(page.getByText(/正在同步最新状态/)).toBeVisible();
  await expect(page.getByRole("heading", { name: "正在同步会话…" })).toHaveCount(0);
  await expect(composer).toBeEnabled();
  await expect(composer).toHaveValue("streaming 期间保留的草稿");
  await expect(page.getByRole("button", { name: "发送" })).toBeDisabled();

  await expect(page.getByText(/正在同步最新状态/)).toHaveCount(0);
  await expect(page.getByText("A", { exact: true })).toBeVisible();
  await expect(composer).toBeEnabled();
});
