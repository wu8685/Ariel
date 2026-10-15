import { expect, test, type Page } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const sessionToken = `s_${"a".repeat(64)}`;
const mermaidMessage = [
  "flowchart TD",
  "    A[HTTP 请求进入 DCG] --> B{SecurityContext<br/>已有身份?}",
  "",
  "    B -- 是 --> C[沿用已有身份]",
  "    B -- 否 --> D[AuthTokenFilter]",
  "",
  "    D --> E{Buservice<br/>存在域账号登录态?}",
  "    E -- 是 --> F[获取 workNo]",
  "    F --> G[查询域账号并同步 DCG 用户]",
  "    G --> H[将 workNo Token<br/>写入 SecurityContext]",
  "    E -- 否 --> I{同时携带<br/>accessId / timestamp / signature?}",
  "    I -- 否 --> J[保持匿名]",
  "    I -- 是 --> K[用 accessId 查询 dcg_api_user]",
  "    K --> L{API 账号存在?}",
  "    L -- 否 --> X[鉴权失败]",
  "    L -- 是 --> M{timestamp 在 UTC<br/>前后 10 分钟内?}",
  "    M -- 否 --> X",
  "    M -- 是 --> N[使用 secretKey 计算<br/>HMAC-SHA1 accessId + timestamp]",
  "    N --> O{签名相同?}",
  "    O -- 否 --> X",
  "    O -- 是 --> P[将 accessId Token<br/>写入 SecurityContext]",
  "    C --> Q{URL 安全规则}",
  "    H --> Q",
  "    P --> Q",
  "    J --> Q",
  "    Q -- permitAll --> R[直接进入 Controller<br/>允许没有身份]",
  "    Q -- authenticated --> S{通过 Spring Security<br/>身份判定?}",
  "    S -- 是 --> T[进入 Controller]",
  "    S -- 否 --> U[未登录处理]",
  "核心结论：",
  "域账号入口依赖 Buservice 登录态，最终以 workNo 表示调用人。",
].join("\n");

const fixtureThread: Thread = {
  threadId: "mermaid-fixture",
  title: "Mermaid 图表验收",
  cwd: "/tmp/mermaid-fixture",
  updatedAt: "2026-10-16T00:00:00Z",
  runtime: "idle",
  turns: [{ turnId: "diagram-turn", status: "completed", items: [{ itemId: "diagram-message", role: "assistant", text: mermaidMessage }] }],
  pendingInteractions: [],
};

async function openMermaidFixture(page: Page) {
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "mermaid", relayEpoch: "mermaid-epoch", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "图表测试 Mac", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } }] };
    if (message.method === "thread.list") data = { threads: [fixtureThread], nextCursor: "" };
    if (message.method === "thread.subscribe") data = { subscriptionId: "sub" };
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    if (message.method === "thread.subscribe") socket.send(JSON.stringify({ type: "event", v: 1, event: "thread.snapshot", deviceId: "mac", threadId: fixtureThread.threadId, subscriptionId: "sub", streamId: "stream", seq: 1, thread: fixtureThread }));
  }));
  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 mermaid-fixture，/tmp/mermaid-fixture" }).click();
  await page.getByText(fixtureThread.title).click();
  await expect(page.getByRole("img", { name: "Mermaid 图表" })).toBeVisible({ timeout: 10_000 });
}

test("mobile renders a bare Mermaid flowchart without swallowing prose or overflowing the page", async ({ page }) => {
  await openMermaidFixture(page);
  await expect(page.getByText("核心结论：", { exact: false })).toBeVisible();
  await expect(page.getByText(/域账号入口依赖 Buservice/)).toBeVisible();
  const geometry = await page.evaluate(() => {
    const bubble = document.querySelector(".message.assistant .message-body");
    const diagram = document.querySelector(".mermaid-diagram");
    return {
      bodyWidth: document.body.scrollWidth,
      viewportWidth: window.innerWidth,
      bubbleRight: bubble?.getBoundingClientRect().right || 0,
      diagramRight: diagram?.getBoundingClientRect().right || 0,
      diagramScrollWidth: diagram?.scrollWidth || 0,
      diagramClientWidth: diagram?.clientWidth || 0,
      diagramScrollLeft: diagram?.scrollLeft || 0,
    };
  });
  expect(geometry.bodyWidth).toBeLessThanOrEqual(geometry.viewportWidth);
  expect(geometry.diagramRight).toBeLessThanOrEqual(geometry.bubbleRight + 1);
  expect(geometry.diagramScrollWidth).toBeGreaterThanOrEqual(geometry.diagramClientWidth);
  expect(geometry.diagramScrollLeft).toBeGreaterThan(0);
  await expect(page).toHaveScreenshot("mobile-inline-mermaid-flowchart.png", { animations: "disabled", caret: "hide", fullPage: true });
});

test("desktop renders the same Mermaid flowchart inside the conversation width", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openMermaidFixture(page);
  const diagram = page.getByRole("img", { name: "Mermaid 图表" });
  const bubble = page.locator(".message.assistant .message-body");
  expect((await diagram.boundingBox())!.width).toBeLessThanOrEqual((await bubble.boundingBox())!.width + 1);
});
