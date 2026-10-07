import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"d".repeat(64)}`;
const markdown = [
  "## 八个模块的内部拆分初稿",
  "",
  "| 一级模块 | 候选子模块 |",
  "| --- | --- |",
  "| **M1 Agent 接入与运行时适配** | 接入配置与运行时探测 |",
  "| **M2 采集协作与证据关联** | 会话历史接入与证据关联 |",
  "| **M3 记忆生产与演化** | 生产任务与版本演化 |",
  "| **M4 记忆内容与元数据管理** | 租户存储与元数据管理 |",
  "| **M5 VFS 检索与上下文访问** | Namespace 与上下文访问 |",
  "| **M6 知识研发与交付协作** | 研发选材与交付协作 |",
  "| **M7 血缘记录与影响追溯** | 加工历史记录与影响追溯 |",
  "| **M8 上下文可观测与效果评估** | 观测与反馈 |",
].join("\n");

const fixtureThread: Thread = {
  threadId: "markdown-table-fixture",
  title: "Markdown 表格排版验收",
  cwd: "/tmp/markdown-table",
  updatedAt: "2026-10-07T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "table-turn", status: "completed", items: [{ itemId: "table-message", role: "assistant", text: markdown }] }],
  pendingInteractions: [],
};

async function openTableFixture(page: Page) {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "markdown-table", relayEpoch: "markdown-table-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "表格测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: fixtureThread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread: fixtureThread }));
  }));

  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 markdown-table，/tmp/markdown-table" }).click();
  await page.getByText(fixtureThread.title).click();
  await expect(page.getByRole("table")).toBeVisible();
  await expect(page.locator(".sidebar")).not.toHaveClass(/\bopen\b/);
}

test("mobile Markdown table keeps every column at one readable font size", async ({ page }) => {
  await openTableFixture(page);
  const scroll = page.locator(".markdown-table-scroll");
  const cells = scroll.locator("th, td");
  await expect(cells).toHaveCount(18);
  const typography = await cells.evaluateAll(elements => elements.map(element => {
    const style = getComputedStyle(element);
    return { fontSize: style.fontSize, lineHeight: style.lineHeight };
  }));
  expect(new Set(typography.map(value => value.fontSize))).toEqual(new Set(["13px"]));
  expect(new Set(typography.map(value => value.lineHeight)).size).toBe(1);
  await expect(scroll).toHaveCSS("text-size-adjust", "100%");

  const geometry = await page.evaluate(() => {
    const body = document.body;
    const bubble = document.querySelector(".message.assistant .message-body");
    const scroll = document.querySelector(".markdown-table-scroll");
    const table = scroll?.querySelector("table");
    return {
      bodyWidth: body.scrollWidth,
      viewportWidth: window.innerWidth,
      bubbleRight: bubble?.getBoundingClientRect().right || 0,
      scrollRight: scroll?.getBoundingClientRect().right || 0,
      scrollWidth: scroll?.scrollWidth || 0,
      scrollClientWidth: scroll?.clientWidth || 0,
      tableWidth: table?.getBoundingClientRect().width || 0,
    };
  });
  expect(geometry.bodyWidth).toBeLessThanOrEqual(geometry.viewportWidth);
  expect(geometry.scrollRight).toBeLessThanOrEqual(geometry.bubbleRight);
  expect(geometry.scrollWidth).toBeGreaterThan(geometry.scrollClientWidth);
  expect(geometry.tableWidth).toBeGreaterThan(geometry.scrollClientWidth);
  await expect(page).toHaveScreenshot("mobile-markdown-table-uniform-type.png", { animations: "disabled", caret: "hide" });
});
