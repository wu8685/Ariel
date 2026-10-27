import { expect, test } from "@playwright/test";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { request as httpRequest } from "node:http";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { Thread } from "../src/generated/protocol";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(webRoot, "..");
const relayPort = 4181;
const relayOrigin = `http://127.0.0.1:${relayPort}`;
const agentToken = "agent-token-for-live-cache-e2e";
const webPIN = "012345";
let scratch = "";
let relay: ChildProcess | null = null;
let agent: WebSocket | null = null;

const completed: Thread = {
  threadId: "live-cache-thread",
  title: "真实 Relay 实时更新",
  cwd: "/tmp/real-relay-cache",
  updatedAt: "2026-10-26T00:00:00Z",
  runtime: "idle",
  historyComplete: false,
  recentComplete: true,
  turns: [{ turnId: "turn-old", status: "completed", items: [{ itemId: "assistant-old", role: "assistant", text: "上一轮已经完成" }] }],
  pendingInteractions: [],
};

function continuingThread(): Thread {
  return {
    ...completed,
    updatedAt: "2026-10-26T00:00:01Z",
    runtime: "inProgress",
    turns: [
      ...completed.turns,
      { turnId: "turn-live", status: "inProgress", items: [
        { itemId: "user-live", role: "user", text: "继续刚才完成的会话" },
        { itemId: "assistant-live", role: "assistant", text: "仍在处理中" },
      ] },
    ],
  };
}

function terminalThread(): Thread {
  return {
    ...continuingThread(),
    updatedAt: "2026-10-26T00:00:02Z",
    runtime: "idle",
    turns: [
      ...completed.turns,
      { turnId: "turn-live", status: "completed", items: [
        { itemId: "user-live", role: "user", text: "继续刚才完成的会话" },
        { itemId: "assistant-live", role: "assistant", text: "无需刷新即可看到最终回复" },
      ] },
    ],
  };
}

async function waitForRelay() {
  for (let attempt = 0; attempt < 80; attempt++) {
    const ready = await new Promise<boolean>(resolveReady => {
      const request = httpRequest(`${relayOrigin}/healthz`, response => {
        response.resume();
        resolveReady(response.statusCode === 200);
      });
      request.on("error", () => resolveReady(false));
      request.end();
    });
    if (ready) return;
    await new Promise(resolveWait => setTimeout(resolveWait, 100));
  }
  throw new Error("live-cache Relay did not become healthy");
}

function sendAgent(value: Record<string, unknown>) {
  if (!agent || agent.readyState !== WebSocket.OPEN) throw new Error("fixture Agent is not connected");
  agent.send(JSON.stringify(value));
}

function cacheSnapshot(thread: Thread, seq: number) {
  const encoded = JSON.stringify(thread);
  return {
    type: "event", v: 1, event: "thread.cache.snapshot", deviceId: "mac", threadId: thread.threadId,
    agentEpoch: "agent-live-cache", cacheStreamId: "stream-live-cache", seq,
    capturedAt: thread.updatedAt, payloadBytes: Buffer.byteLength(encoded), thread,
  };
}

async function connectAgent() {
  agent = new WebSocket(`ws://127.0.0.1:${relayPort}/ws`);
  await new Promise<void>((resolveConnected, rejectConnected) => {
    const fail = () => rejectConnected(new Error("fixture Agent connection failed"));
    agent!.addEventListener("error", fail, { once: true });
    agent!.addEventListener("open", () => sendAgent({
      type: "hello", v: 1, role: "agent", token: agentToken, deviceId: "mac", deviceName: "真实 Relay 测试 Mac",
      agentEpoch: "agent-live-cache", adapterVersion: "e2e", capabilities: { autoLoad: true, codexReady: true, history: true, send: true },
    }), { once: true });
    agent!.addEventListener("message", event => {
      const message = JSON.parse(String(event.data));
      if (message.type === "hello.ok") {
        sendAgent(cacheSnapshot(completed, 1));
        resolveConnected();
        return;
      }
      if (message.type !== "request") return;
      let data: Record<string, unknown> = {};
      if (message.method === "thread.list") data = { threads: [completed], nextCursor: "" };
      if (message.method === "thread.read") data = { thread: completed };
      sendAgent({ type: "response", v: 1, requestId: message.requestId, outcome: "accepted", data });
    });
  });
}

test.beforeAll(async () => {
  scratch = mkdtempSync(join(tmpdir(), "ariel-live-cache-e2e-"));
  const relayBinary = join(scratch, "ariel-relay");
  const build = spawnSync("go", ["build", "-o", relayBinary, "./cmd/relay"], { cwd: repoRoot, stdio: "pipe", env: { ...process.env, GO111MODULE: "on", GOTOOLCHAIN: "auto" } });
  if (build.status !== 0) throw new Error(build.stderr.toString() || build.stdout.toString());
  relay = spawn(relayBinary, [], {
    cwd: repoRoot,
    stdio: "pipe",
    env: {
      ...process.env,
      ARIEL_TOKEN: agentToken,
      ARIEL_WEB_PIN: webPIN,
      ARIEL_ORIGINS: relayOrigin,
      ARIEL_WEB_DIST: join(webRoot, "dist"),
      ARIEL_LISTEN: `127.0.0.1:${relayPort}`,
    },
  });
  await waitForRelay();
  await connectAgent();
});

test.afterAll(async () => {
  agent?.close();
  relay?.kill("SIGTERM");
  if (relay && relay.exitCode === null) await new Promise(resolveExit => relay?.once("exit", resolveExit));
  if (scratch) rmSync(scratch, { recursive: true, force: true });
});

test("a completed conversation continues and reaches terminal through the real Relay without reload", async ({ page }, info) => {
  const frames: string[] = [];
  page.on("websocket", socket => socket.on("framereceived", event => frames.push(String(event.payload))));
  try {
  await page.goto(relayOrigin);
  await page.getByLabel("6 位连接码").fill(webPIN);
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await page.getByRole("button", { name: "展开项目 real-relay-cache，/tmp/real-relay-cache" }).click();
  await page.getByText(completed.title).click();
  await expect(page.getByText("上一轮已经完成")).toBeVisible();

  // A valid lease can restore freshness without a new content snapshot. This
  // goes through the real Relay, including its explicit-content-error fence.
  sendAgent(cacheSnapshot(completed, 2));
  await page.getByLabel("发送消息").fill("恢复期间保留的草稿");
  sendAgent({ type: "event", v: 1, event: "thread.cache.status", deviceId: "mac", threadId: completed.threadId,
    agentEpoch: "agent-live-cache", cacheStreamId: "stream-live-cache", freshness: "error", reason: "follower_lease_expired" });
  await expect(page.getByRole("button", { name: "正在同步会话，暂不可发送" })).toBeDisabled();
  sendAgent({ type: "event", v: 1, event: "thread.cache.status", deviceId: "mac", threadId: completed.threadId,
    agentEpoch: "agent-live-cache", cacheStreamId: "stream-live-cache", freshness: "fresh" });
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  await expect(page.getByLabel("发送消息")).toHaveValue("恢复期间保留的草稿");

  sendAgent(cacheSnapshot(continuingThread(), 3));
  await expect(page.getByText("继续刚才完成的会话")).toBeVisible();
  await expect(page.getByText("仍在处理中")).toBeVisible();
  await expect(page.locator(".runtime")).toHaveText("运行中");

  // A one-shot recovery refresh may fail even though the original native
  // follower remains healthy. Relay/Web must keep the old body read-only and
  // allow a later snapshot on the same stream to take over automatically.
  sendAgent({
    type: "event", v: 1, event: "thread.cache.status", deviceId: "mac",
    threadId: completed.threadId, agentEpoch: "agent-live-cache", cacheStreamId: "stream-live-cache",
    freshness: "error", reason: "RESYNC_REQUIRED",
  });
  await expect(page.locator(".send-button")).toBeDisabled();

  sendAgent({ type: "event", v: 1, event: "thread.cache.status", deviceId: "mac", threadId: completed.threadId,
    agentEpoch: "agent-live-cache", cacheStreamId: "stream-live-cache", freshness: "fresh" });
  // Check Relay's content-error fence as well as the disabled Web control.
  const readOnly = await page.evaluate(async () => {
    const socket = new WebSocket(`${location.origin.replace(/^http/, "ws")}/ws`);
    return await new Promise<boolean>((resolve, reject) => {
      const timer = window.setTimeout(() => { socket.close(); reject(new Error("read barrier timed out")); }, 5000);
      socket.onopen = () => socket.send(JSON.stringify({ type: "hello", v: 1, role: "web", token: "012345" }));
      socket.onmessage = event => {
        const value = JSON.parse(String(event.data));
        if (value.type === "hello.ok") socket.send(JSON.stringify({ type: "request", v: 1, requestId: crypto.randomUUID(), method: "thread.cache.read", deviceId: "mac", params: { threadId: "live-cache-thread" } }));
        if (value.type === "response") { window.clearTimeout(timer); socket.close(); resolve(value.data?.freshness === "stale" && value.data?.staleReason === "RESYNC_REQUIRED"); }
      };
      socket.onerror = () => { window.clearTimeout(timer); socket.close(); reject(new Error("read barrier failed")); };
    });
  });
  expect(readOnly).toBe(true);
  await expect(page.locator(".send-button")).toBeDisabled();

  sendAgent(cacheSnapshot(terminalThread(), 4));

  await expect(page.getByText("无需刷新即可看到最终回复")).toBeVisible();
  await expect(page.locator(".runtime")).toHaveText("待命");
  await page.getByLabel("发送消息").fill("终态到达后恢复发送");
  await expect(page.getByRole("button", { name: "发送" })).toBeEnabled();
  } finally {
    if (info.status !== info.expectedStatus || test.info().errors.length) await info.attach("isolated-relay-frames", { body: frames.join("\n"), contentType: "application/x-ndjson" });
  }
});
