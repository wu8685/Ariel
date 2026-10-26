import { expect, test, type WebSocketRoute } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";

const thread: Thread = {
  threadId: "socket-generation",
  title: "连接生命周期回归",
  cwd: "/work/socket-fixture",
  updatedAt: "2026-10-26T00:00:00Z",
  runtime: "idle",
  pendingInteractions: [],
  turns: [
    {
      turnId: "turn",
      status: "completed",
      items: [{ itemId: "reply", role: "assistant", text: "已确认的原正文" }],
    },
  ],
};

test("a delayed retired handshake cannot break foreground recovery or require a new Passkey login", async ({
  page,
}) => {
  // Run the production client in Chromium with isolated WS routing. Delay one
  // callback to fix the race window; this is not a physical Safari packet trace.
  await page.route("**/", async (route) => {
    const response = await route.fetch();
    await route.fulfill({
      response,
      body: (await response.text()).replace("__ARIEL_WEB_AUTH__", "passkey"),
    });
  });
  let authChecks = 0;
  await page.route("**/api/auth/**", (route) => {
    expect(new URL(route.request().url()).pathname).toBe("/api/auth/status");
    expect(route.request().method()).toBe("GET");
    authChecks++;
    return route.fulfill({
      json: { mode: "passkey", authenticated: true, enrollmentRequired: false },
    });
  });
  let current: WebSocketRoute | null = null,
    connections = 0,
    watches = 0;
  let deferHello = false,
    releaseFresh: (() => void) | null = null;
  const methods: string[] = [];
  await page.routeWebSocket("**/ws", (socket) => {
    const connection = ++connections;
    let handshaken = false;
    socket.onMessage((raw) => {
      const message = JSON.parse(String(raw));
      if (message.type === "hello") {
        current = socket;
        const acknowledge = () => {
          handshaken = true;
          socket.send(
            JSON.stringify({
              type: "hello.ok",
              v: 1,
              connectionId: `connection-${connection}`,
              relayEpoch: "relay",
            }),
          );
        };
        if (deferHello) releaseFresh = acknowledge;
        else acknowledge();
        return;
      }
      if (message.type !== "request" || !handshaken) return;
      methods.push(message.method);
      let data: Record<string, unknown> = {};
      if (message.method === "device.list")
        data = {
          devices: [
            {
              deviceId: "mac",
              deviceName: "Socket Mac",
              agentOnline: true,
              codexReady: true,
              agentEpoch: "agent",
              capabilities: { autoLoad: true, send: true },
            },
          ],
        };
      if (message.method === "thread.list")
        data = { threads: [thread], nextCursor: "" };
      if (message.method === "thread.cache.watch") {
        watches++;
        const fresh =
          watches === 1
            ? thread
            : {
                ...thread,
                turns: [
                  ...thread.turns,
                  {
                    turnId: "resumed",
                    status: "completed",
                    items: [
                      {
                        itemId: "resumed-reply",
                        role: "assistant",
                        text: "恢复后的新正文",
                      },
                    ],
                  },
                ],
              };
        data = {
          watchId: `watch-${connection}`,
          freshness: "fresh",
          agentOnline: true,
          agentEpoch: "agent",
          cacheStreamId: `stream-${connection}`,
          seq: 1,
          capturedAt: thread.updatedAt,
          thread: fresh,
        };
      }
      socket.send(
        JSON.stringify({
          type: "response",
          v: 1,
          requestId: message.requestId,
          outcome: "accepted",
          data,
        }),
      );
    });
  });
  await page.goto("/");
  await page
    .getByRole("button", {
      name: "展开项目 socket-fixture，/work/socket-fixture",
    })
    .click();
  await page.getByText(thread.title, { exact: true }).click();
  await expect(page.getByText("已确认的原正文")).toBeVisible();
  const input = page.getByRole("textbox", { name: "发送消息" });
  await input.fill("重连期间保留的草稿");
  // Install after Playwright has initialized its WS routing. No application
  // hooks are added: only the next transport's message callback is delayed.
  await page.evaluate(() => {
    const NativeSocket = window.WebSocket;
    const state = {
      holdNextHello: false,
      release: null as (() => void) | null,
    };
    Object.assign(window, { __arielDelayedSocket: state });
    window.WebSocket = class extends NativeSocket {
      constructor(url: string | URL, protocols?: string | string[]) {
        super(url, protocols);
        let handler: WebSocket["onmessage"] = null;
        this.addEventListener("message", (event) => {
          const next = handler;
          if (
            next &&
            state.holdNextHello &&
            JSON.parse(event.data).type === "hello.ok"
          ) {
            state.holdNextHello = false;
            state.release = () => next.call(this, event);
          } else next?.call(this, event);
        });
        Object.defineProperty(this, "onmessage", {
          get: () => null,
          set: (next: WebSocket["onmessage"]) => {
            handler = next;
          },
        });
      }
    };
  });
  await page.evaluate(() => {
    (window as any).__arielDelayedSocket.holdNextHello = true;
  });
  current!.close({ code: 1012, reason: "fixture reconnect" });
  await expect
    .poll(() =>
      page.evaluate(() =>
        Boolean((window as any).__arielDelayedSocket.release),
      ),
    )
    .toBe(true);
  deferHello = true;
  await page.evaluate(() => {
    let visibility = "hidden";
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => visibility,
    });
    document.dispatchEvent(new Event("visibilitychange"));
    const now = Date.now;
    try {
      Date.now = () => now() + 6000;
      visibility = "visible";
      document.dispatchEvent(new Event("visibilitychange"));
    } finally {
      Date.now = now;
    }
  });
  await expect.poll(() => Boolean(releaseFresh)).toBe(true);
  // The hello from the now-retired second transport is processed immediately
  // before the third transport receives its legitimate hello.
  await page.evaluate(() => {
    const state = (window as any).__arielDelayedSocket;
    const deliver = state.release;
    state.release = null;
    deliver();
  });
  releaseFresh!();
  await expect(page.getByText("恢复后的新正文")).toBeVisible();
  await expect(page.getByText("已确认的原正文")).toBeVisible();
  await expect(input).toHaveValue("重连期间保留的草稿");
  await expect(
    page.getByRole("button", { name: "发送", exact: true }),
  ).toBeEnabled();
  await expect(
    page.getByRole("button", { name: "使用 Passkey 登录" }),
  ).toHaveCount(0);
  await expect(page.getByText(/页面版本与 Relay 不兼容/)).toHaveCount(0);
  expect(authChecks).toBeGreaterThanOrEqual(2);
  expect(
    methods.some(
      (method) => method.startsWith("turn.") || method.startsWith("queue."),
    ),
  ).toBe(false);
});
