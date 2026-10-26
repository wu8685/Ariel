import { expect, test } from "@playwright/test";

const sessionToken = `s_${"c".repeat(64)}`;
const targetThread = {
  threadId: "target-thread",
  title: "目标设备会话",
  cwd: "/tmp/target-device",
  updatedAt: "2026-10-11T00:00:00Z",
  runtime: "idle",
  turns: [],
  pendingInteractions: [],
};

test("device switching keeps a cold read pending beyond the old 12 second deadline", async ({ page }) => {
  await page.clock.install();
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  let sendResponse: ((deviceId: string, response: Record<string, unknown>) => void) | null = null;
  const listRequests = new Map<string, string>();
  await page.routeWebSocket("**/ws", socket => {
    sendResponse = (deviceId, response) => {
      const requestId = listRequests.get(deviceId);
      if (!requestId) throw new Error(`missing list request for ${deviceId}`);
      socket.send(JSON.stringify({ type: "response", v: 1, requestId, ...response }));
    };
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "switch", relayEpoch: "switch-epoch", sessionToken }));
        return;
      }
      if (message.type !== "request") return;
      if (message.method === "device.list") {
        socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data: { devices: [
          { deviceId: "mac-a", deviceName: "Mac A", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } },
          { deviceId: "mac-b", deviceName: "Mac B", agentOnline: true, codexReady: true, capabilities: { autoLoad: true } },
        ] } }));
      }
      if (message.method === "thread.list") listRequests.set(message.deviceId, message.requestId);
    });
  });

  await page.goto("/");
  await expect.poll(() => listRequests.has("mac-a")).toBe(true);
  await page.getByRole("combobox",{name:"设备"}).click(); await page.getByRole("option",{name:"Mac B"}).click();
  await expect.poll(() => listRequests.has("mac-b")).toBe(true);

  await page.clock.fastForward(13_000);
  await expect(page.getByText("正在加载会话…")).toBeVisible();
  sendResponse!("mac-a", { outcome: "unknown", error: { code: "OUTCOME_UNKNOWN", message: "late A" } });
  await expect(page.getByRole("alert")).toHaveCount(0);
  sendResponse!("mac-b", { outcome: "accepted", data: { threads: [targetThread], nextCursor: "" } });

  await expect(page.getByRole("button", { name: /展开项目 target-device/ })).toBeVisible();
  await page.getByRole("button", { name: /展开项目 target-device/ }).click();
  await expect(page.getByText(targetThread.title)).toBeVisible();
});
