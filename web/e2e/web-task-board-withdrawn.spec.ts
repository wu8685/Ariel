import { expect, test, type Page } from "@playwright/test";

const sessionToken = `s_${"9".repeat(64)}`;

async function openConversationOnlyWeb(page: Page) {
  const requests: Record<string, unknown>[] = [];
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => {
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "conversation-only", relayEpoch: "conversation-only-epoch", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      requests.push(message);
      const data = message.method === "device.list"
        ? { devices: [{ deviceId: "desktop", deviceName: "隔离测试 Mac", taskStoreId: "store-fixture", agentEpoch: "desktop-1", agentOnline: true, codexReady: true, capabilities: { autoLoad: true, codexReady: true, taskManagement: true, taskExecution: true } }] }
        : message.method === "thread.list"
          ? { threads: [] }
          : {};
      socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
    });
  });
  await page.goto("/");
  await expect(page.getByRole("button", { name: "打开会话列表", exact: false })).toBeVisible();
  await expect.poll(() => requests.some(request => request.method === "thread.list")).toBe(true);
  return requests;
}

function expectNoTaskRequests(requests: Record<string, unknown>[]) {
  expect(requests.filter(request => typeof request.method === "string" && request.method.startsWith("task."))).toHaveLength(0);
}

test("mobile Web returns to the conversation-only sidebar", async ({ page }) => {
  const requests = await openConversationOnlyWeb(page);
  await expect(page.getByRole("complementary", { name: "会话列表" })).toBeVisible();
  await expect(page.getByRole("button", { name: "任务看板", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "进入任务看板", exact: true })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "任务看板" })).toHaveCount(0);
  expectNoTaskRequests(requests);
  await expect(page).toHaveScreenshot("conversation-only-mobile.png", { animations: "disabled" });
});

test("desktop Web has no task mode even when the backend advertises task capabilities", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const requests = await openConversationOnlyWeb(page);
  await expect(page.getByRole("complementary", { name: "会话列表" })).toBeVisible();
  await expect(page.getByRole("button", { name: "任务看板", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "进入任务看板", exact: true })).toHaveCount(0);
  expectNoTaskRequests(requests);
  await expect(page).toHaveScreenshot("conversation-only-desktop.png", { animations: "disabled" });
});
