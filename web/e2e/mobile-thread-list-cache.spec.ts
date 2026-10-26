import { expect, test } from "@playwright/test";

const sessionToken = `s_${"c".repeat(64)}`;
const makeThread = (device: string) => ({
  threadId: `thread-${device}`,
  title: `设备 ${device.toUpperCase()} 的缓存会话`,
  cwd: `/tmp/device-${device}`,
  updatedAt: "2026-10-11T00:00:00Z",
  runtime: "idle",
  turns: [],
  pendingInteractions: [],
});

test("switching back to a device restores its fresh thread list without another request or blank state", async ({ page }) => {
  let listRequests = 0;
  await page.addInitScript(token => sessionStorage.setItem("ariel.web-session.v1", token), sessionToken);
  await page.routeWebSocket("**/ws", socket => socket.onMessage(raw => {
    const message = JSON.parse(String(raw));
    if (message.type === "hello") {
      socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: "thread-cache", relayEpoch: "relay-cache", sessionToken }));
      return;
    }
    if (message.type !== "request") return;
    let data: Record<string, unknown> = {};
    if (message.method === "device.list") data = { devices: [
      { deviceId: "mac-a", deviceName: "Mac A", agentOnline: true, codexReady: true, agentEpoch: "epoch-a", capabilities: { autoLoad: true } },
      { deviceId: "mac-b", deviceName: "Mac B", agentOnline: true, codexReady: true, agentEpoch: "epoch-b", capabilities: { autoLoad: true } },
    ] };
    if (message.method === "thread.list") {
      listRequests++;
      data = { threads: [makeThread(message.deviceId === "mac-a" ? "a" : "b")], nextCursor: "" };
    }
    socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
  }));

  await page.goto("/");
  await expect(page.getByRole("button", { name: "展开项目 device-a，/tmp/device-a" })).toBeVisible();
  expect(listRequests).toBe(1);

  await page.getByRole("combobox",{name:"设备"}).click(); await page.getByRole("option",{name:"Mac B"}).click();
  await expect(page.getByRole("button", { name: "展开项目 device-b，/tmp/device-b" })).toBeVisible();
  expect(listRequests).toBe(2);

  await page.getByRole("combobox",{name:"设备"}).click(); await page.getByRole("option",{name:"Mac A"}).click();
  const cachedProject = page.getByRole("button", { name: "展开项目 device-a，/tmp/device-a" });
  await expect(cachedProject).toBeVisible();
  await cachedProject.click();
  await expect(page.getByText("设备 A 的缓存会话")).toBeVisible();
  await page.waitForTimeout(250);
  expect(listRequests).toBe(2);
  await expect(page).toHaveScreenshot("mobile-device-thread-list-restored.png", { animations: "disabled", caret: "hide" });
});
