import { expect, test } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"d".repeat(64)}`;
const initial: Thread = {
  threadId: "00000000-0000-4000-8000-000000000075",
  title: "Metadata fixture",
  cwd: "/tmp/metadata-fixture",
  updatedAt: "2026-10-13T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "turn", status: "completed", items: [{ itemId: "answer", role: "assistant", text: "正文保持可见" }] }],
  pendingInteractions: [],
};

test("phone long press renames and pins without replacing the open conversation", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let current = initial;
  let renameCount = 0;
  let pinCount = 0;
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "metadata", relayEpoch: "relay", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Metadata Mac", agentOnline: true, codexReady: true, agentEpoch: "epoch", capabilities: { autoLoad: true, threadRename: true, threadPin: true } }] };
    if (message.method === "thread.list") data = { threads: [current], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    if (message.method === "thread.name.update") {
      renameCount++;
      current = { ...current, title: message.params.name };
      data = { thread: { ...current, turns: [], pendingInteractions: [] } };
    }
    if (message.method === "thread.pin.update") {
      pinCount++;
      current = { ...current, isPinned: message.params.pinned };
      data = { thread: { ...current, turns: [], pendingInteractions: [] } };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: current.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread: current }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 metadata-fixture，/tmp/metadata-fixture" }).click();
  await page.getByText(initial.title).click();
  await expect(page.getByText("正文保持可见")).toBeVisible();
  await page.getByRole("textbox", { name: "发送消息" }).fill("保留草稿");
  await page.getByRole("button", { name: /打开会话列表/ }).click();

  const longPress = async () => {
    const row = page.locator(`.thread-row[data-thread-id="${initial.threadId}"]`);
    await row.dispatchEvent("pointerdown", { pointerId: 7, pointerType: "touch", button: 0, clientX: 120, clientY: 260 });
    await page.waitForTimeout(550);
    return row;
  };

  await longPress();
  const rename = page.getByRole("menuitem", { name: "重命名" });
  await expect(rename).toHaveCSS("font-size", "12px");
  await rename.click();
  await page.getByRole("textbox", { name: "会话名称" }).fill("手机改名");
  await page.getByRole("button", { name: "保存名称" }).click();
  await expect.poll(() => renameCount).toBe(1);
  await expect(page.getByRole("heading", { name: "手机改名" })).toBeVisible();
  await expect(page.getByText("正文保持可见")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "发送消息" })).toHaveValue("保留草稿");

  await longPress();
  await page.getByRole("menuitem", { name: "置顶" }).click();
  await expect.poll(() => pinCount).toBe(1);
  await expect(page.getByRole("region", { name: "置顶会话" })).toBeVisible();
  await expect(page.getByText("正文保持可见")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "发送消息" })).toHaveValue("保留草稿");
});
