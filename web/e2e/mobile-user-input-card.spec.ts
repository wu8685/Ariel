import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"e".repeat(64)}`;
const questionThread = {
  threadId: "question-thread",
  title: "需要补充输入",
  cwd: "/tmp/user-input",
  updatedAt: "2026-10-20T00:00:00Z",
  runtime: "inProgress",
  turns: [{ turnId: "turn-question", status: "inProgress", items: [{ itemId: "assistant-question", role: "assistant", text: "我需要确认两个细节。" }] }],
  pendingInteractions: [{
    interactionId: "request-user-input-1",
    kind: "user_input",
    prompt: "请回答 Codex 的补充问题",
    availableDecisions: ["answer"],
    questions: [
      { id: "mode", header: "执行方式", question: "这次如何继续？", options: [{ label: "继续", description: "按当前方案继续实现" }, { label: "暂停", description: "暂时停止本轮工作" }] },
      { id: "note", header: "补充说明", question: "还有什么需要注意？" },
    ],
  }],
} as Thread;

const otherThread: Thread = {
  threadId: "other-thread",
  title: "另一个会话",
  cwd: "/tmp/user-input",
  updatedAt: "2026-10-19T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "other-turn", status: "completed", items: [{ itemId: "other-answer", role: "assistant", text: "另一个会话的内容" }] }],
  pendingInteractions: [],
};

test("mobile answers a structured request once and keeps the draft while switching conversations", async ({ page }) => {
  let relaySocket: { send(data: string): void } | null = null;
  let pendingResponseId = "";
  const interactionRequests: Array<Record<string, unknown>> = [];
  let subscriptionCount = 0;

  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    relaySocket = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "input-card", relayEpoch: "relay-input", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "interaction.respond") {
        interactionRequests.push(message);
        pendingResponseId = message.requestId;
        return;
      }
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "输入测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, interaction: true } }] };
      if (message.method === "thread.list") data = { threads: [questionThread, otherThread], nextCursor: "" };
      if (message.method === "thread.subscribe") data = { subscriptionId: `sub-${++subscriptionCount}` };
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
      if (message.method === "thread.subscribe") {
        const thread = message.params.threadId === questionThread.threadId ? questionThread : otherThread;
        socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: thread.threadId, subscriptionId: `sub-${subscriptionCount}`, streamId: `stream-${subscriptionCount}`, seq: 1, thread }));
      }
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 user-input，/tmp/user-input" }).click();
  await page.getByText(questionThread.title, { exact: true }).click();
  await page.getByRole("radio", { name: "继续" }).click();
  await page.getByRole("textbox", { name: "还有什么需要注意？" }).fill("保留手机端草稿");
  await expect(page.locator(".interaction-card")).toHaveScreenshot("mobile-user-input-card.png", { animations: "disabled", caret: "hide" });

  await page.getByRole("button", { name: "打开会话列表" }).click();
  await page.getByText(otherThread.title, { exact: true }).click();
  await expect(page.getByText("另一个会话的内容")).toBeVisible();
  await page.getByRole("button", { name: "打开会话列表" }).click();
  await page.getByText(questionThread.title, { exact: true }).click();
  await expect(page.getByRole("radio", { name: "继续" })).toBeChecked();
  await expect(page.getByRole("textbox", { name: "还有什么需要注意？" })).toHaveValue("保留手机端草稿");

  await page.getByRole("button", { name: "提交回答" }).click();
  await expect.poll(() => interactionRequests.length).toBe(1);
  expect(interactionRequests[0]).toMatchObject({ params: { threadId: questionThread.threadId, interactionId: "request-user-input-1", decision: "answer", answers: { mode: ["继续"], note: ["保留手机端草稿"] } } });
  await expect(page.getByRole("button", { name: "正在提交…" })).toBeDisabled();
  await expect(page.getByRole("radio", { name: "继续" })).toBeDisabled();

  relaySocket!.send(JSON.stringify({ type: "response", v: 1, requestId: pendingResponseId, outcome: "accepted", data: {} }));
  await expect(page.getByRole("button", { name: "提交回答" })).toBeEnabled();
  expect(interactionRequests).toHaveLength(1);

  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(page.locator(".interaction-card")).toHaveScreenshot("desktop-user-input-card.png", { animations: "disabled", caret: "hide" });
  const cardBox = await page.locator(".interaction-card").boundingBox();
  expect(cardBox?.width || 0).toBeGreaterThan(500);
  expect((cardBox?.x || 0) + (cardBox?.width || 0)).toBeLessThanOrEqual(1280);
});
