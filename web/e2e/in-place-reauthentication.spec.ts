import { expect, test, type WebSocketRoute } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

test("expired Passkey recovers in the same mobile conversation without losing the draft", async ({ page }) => {
  await page.route("**/", async route => {
    const response = await route.fetch();
    await route.fulfill({ response, body: (await response.text()).replace("__ARIEL_WEB_AUTH__", "passkey") });
  });
  let authenticated = true, loginCalls = 0;
  await page.route("**/api/auth/**", route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/status")) return route.fulfill({ json: { mode: "passkey", authenticated, enrollmentRequired: false } });
    if (path.endsWith("/login/options")) {
      loginCalls++;
      return route.fulfill({ json: { publicKey: { challenge: "AQI", rpId: "localhost", userVerification: "required" } } });
    }
    expect(path).toBe("/api/auth/passkey/login/verify");
    authenticated = true;
    return route.fulfill({ json: { ok: true } });
  });
  // Isolated ceremony fixture: actual WebAuthn cryptography remains covered by
  // passkey-auth.spec.ts. Never sends to a real Codex business conversation.
  await page.addInitScript(() => {
    let calls = 0;
    Object.defineProperty(navigator, "credentials", { configurable: true, value: {
      get: async () => {
        if (++calls === 1) throw new DOMException("cancel", "NotAllowedError");
        return { type: "public-key", toJSON: () => ({ id: "fixture", type: "public-key" }) };
      },
    } });
  });
  const thread: Thread = { threadId: "fixture", title: "原地续登录", cwd: "/work/fixture", updatedAt: "2026-10-26T00:00:00Z", runtime: "idle", pendingInteractions: [], turns: [{ turnId: "t", status: "completed", items: [{ itemId: "a", role: "assistant", text: "登录失效也应保留的正文" }] }] };
  let current: WebSocketRoute | null = null, connections = 0;
  let releaseWatch: (() => void) | null = null;
  const methods: string[] = [];
  await page.routeWebSocket("**/ws", socket => {
    const connection = ++connections;
    current = socket;
    socket.onMessage(raw => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        socket.send(JSON.stringify({ type: "hello.ok", v: 1, connectionId: `c${connection}`, relayEpoch: "relay" }));
        return;
      }
      if (message.type !== "request") return;
      methods.push(message.method);
      let data: Record<string, unknown> = {};
      if (message.method === "device.list") data = { devices: [{ deviceId: "mac", deviceName: "Mac", agentOnline: true, codexReady: true, agentEpoch: "agent", capabilities: { autoLoad: true, send: true } }] };
      if (message.method === "thread.list") data = { threads: [thread] };
      if (message.method === "thread.cache.watch") data = { watchId: `w${connection}`, freshness: "fresh", agentOnline: true, agentEpoch: "agent", cacheStreamId: `s${connection}`, seq: 1, capturedAt: thread.updatedAt, thread: connection === 1 ? thread : { ...thread, turns: [...thread.turns, { turnId: "resumed", status: "completed", items: [{ itemId: "b", role: "assistant", text: "续登录后的最新正文" }] }] } };
      const respond = () => socket.send(JSON.stringify({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data }));
      if (message.method === "thread.cache.watch" && connection > 1) releaseWatch = respond;
      else respond();
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "展开项目 fixture，/work/fixture" }).click();
  await page.getByText(thread.title, { exact: true }).click();
  const input = page.getByRole("textbox", { name: "发送消息" });
  await expect(page.getByText("登录失效也应保留的正文")).toBeVisible();
  await input.fill("认证期间继续保留的草稿");
  authenticated = false;
  current!.close({ code: 1008, reason: "unauthorized" });
  const banner = page.getByRole("region", { name: "恢复登录" });
  await expect(banner).toBeVisible();
  await expect(page.getByRole("region", { name: "Passkey 登录" })).toHaveCount(0);
  await expect(page.getByText("登录失效也应保留的正文")).toBeVisible();
  await expect(input).toHaveValue("认证期间继续保留的草稿");
  await expect(input).toBeEditable();
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  expect(loginCalls).toBe(0);
  const geometry = await page.evaluate(() => {
    const box = (selector: string) => { const r = document.querySelector(selector)!.getBoundingClientRect(); return { top: r.top, bottom: r.bottom, width: r.width }; };
    return { head: box(".conversation-head"), banner: box(".reauth-banner"), transcript: box(".transcript"), composer: box(".composer-wrap"), width: innerWidth, scrollWidth: document.documentElement.scrollWidth };
  });
  expect(geometry.head.bottom - geometry.head.top).toBe(52);
  expect(geometry.transcript.top).toBeGreaterThanOrEqual(geometry.banner.bottom);
  expect(geometry.transcript.bottom).toBeLessThanOrEqual(geometry.composer.top + 1);
  expect(geometry.scrollWidth).toBeLessThanOrEqual(geometry.width);
  await expect(page).toHaveScreenshot("mobile-inline-reauth.png");
  await page.setViewportSize({ width: 320, height: 844 });
  await expect(banner.getByRole("button", { name: "重新登录" })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  await page.setViewportSize({ width: 390, height: 844 });
  await banner.getByRole("button", { name: "重新登录" }).click();
  await expect(page.getByText("Passkey 操作已取消；你可以重新尝试。")).toBeVisible();
  await expect(input).toHaveValue("认证期间继续保留的草稿");
  await input.fill("取消后仍能继续编辑");
  await banner.getByRole("button", { name: "重新登录" }).click();
  await expect.poll(() => releaseWatch !== null).toBe(true);
  await expect(page.getByText("登录失效也应保留的正文")).toBeVisible();
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  expect(methods.filter(method => method === "turn.start")).toHaveLength(0);
  releaseWatch!();
  await expect(page.getByText("续登录后的最新正文")).toBeVisible();
  await expect(banner).toHaveCount(0);
  await expect(input).toHaveValue("取消后仍能继续编辑");
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  expect(connections).toBe(2);
  expect(loginCalls).toBe(2);
});
