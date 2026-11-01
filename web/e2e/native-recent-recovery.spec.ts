import { expect, test, type Page } from "@playwright/test";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const origin = "http://127.0.0.1:4184";
let scratch = "", binary = "", output = "";
let fixture: ChildProcess | null = null;

test.beforeAll(() => {
  scratch = mkdtempSync(join(tmpdir(), "ariel-recent-native-e2e-"));
  binary = process.env.ARIEL_RECENT_FIXTURE_BINARY || join(scratch, "cache-recovery");
  if (!process.env.ARIEL_RECENT_FIXTURE_BINARY) {
    // Exercise production-size payload latency with the production binary.
    // Race instrumentation's per-byte shadow memory distorts a 50 MiB parser
    // workload; Go's separate race suite covers the concurrent code paths.
    const build = spawnSync("go", ["build", "-o", binary, "./tests/fixtures/cache-recovery"], { cwd: resolve(webRoot, ".."), stdio: "pipe" });
    if (build.status !== 0) throw new Error(build.stderr.toString());
  }
});
test.beforeEach(async () => {
  output = "";
  fixture = spawn(binary, ["-listen", "127.0.0.1:4184", "-dist", join(webRoot, "dist"), "-native-wire", "-recent-recovery"], { stdio: "pipe" });
  fixture.stderr?.on("data", chunk => { output += String(chunk); });
  for (let attempt = 0; attempt < 100; attempt++) {
    try { if ((await fetch(`${origin}/fixture/state`)).ok) return; } catch { /* starting */ }
    if (fixture.exitCode !== null) throw new Error(output);
    await new Promise(done => setTimeout(done, 100));
  }
  throw new Error(`native recent fixture did not start: ${output}`);
});
test.afterEach(async ({}, info) => {
  fixture?.kill("SIGTERM");
  if (fixture && fixture.exitCode === null) await new Promise(done => fixture?.once("exit", done));
  if (info.status !== info.expectedStatus) await info.attach("isolated-agent-log", { body: output, contentType: "text/plain" });
  expect(output).not.toContain("DATA RACE");
});
test.afterAll(() => { if (scratch) rmSync(scratch, { recursive: true, force: true }); });

async function openNativeThread(page: Page) {
  await page.goto(origin);
  await page.getByLabel("6 位连接码").fill("012345");
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await page.getByRole("button", { name: "展开项目 native-wire-regression，/tmp/native-wire-regression" }).click();
  await page.getByText("原生大帧回归").click();
  await expect(page.getByText("原生大帧首次打开成功")).toBeVisible({ timeout: 30_000 });
}

test("fifty MiB of native old bodies are retired while recent text and tools still update", async ({ page, request }) => {
  test.setTimeout(60_000);
  await openNativeThread(page);
  const initial = await (await request.get(`${origin}/fixture/state`)).json();
  expect(initial.native.wireBytes).toBeGreaterThan(50 * 1024 * 1024);
  expect(initial.native.wireBytes).toBeLessThan(64 * 1024 * 1024);
  expect(initial.residentBytes).toBeLessThan(2 * 1024 * 1024);
  await page.getByLabel("发送消息").fill("旧正文回收期间保留草稿");
  for (const [action, text] of [["stream", "原生大帧后续增量已到达"], ["complete", "原生大帧最终回复已完成"]]) {
    expect((await request.post(`${origin}/fixture/action?action=${action}`)).ok()).toBe(true);
    await expect(page.getByText(text)).toBeVisible();
    await expect(page.getByLabel("发送消息")).toHaveValue("旧正文回收期间保留草稿");
  }
  const final = await (await request.get(`${origin}/fixture/state`)).json();
  expect(final.attaches).toBe(1);
  expect(final.native.loads).toBe(initial.native.loads);
  expect(final.native.unexpected).toBe(0);
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
});

test("native revision failure recovers completion without browser refresh, promotion or socket reconnect", async ({ page, request }) => {
  test.setTimeout(60_000);
  let navigations = 0, sockets = 0, promotions = 0, mutations = 0;
  page.on("framenavigated", frame => { if (frame === page.mainFrame()) navigations++; });
  page.on("websocket", socket => {
    sockets++;
    socket.on("framesent", frame => {
      const data = JSON.parse(String(frame.payload));
      if (data.method === "thread.cache.promote") promotions++;
      if (["turn.start", "turn.interrupt", "interaction.respond"].includes(data.method)) mutations++;
    });
  });
  await openNativeThread(page);
  await page.getByLabel("发送消息").fill("自动恢复必须保留的草稿");
  expect((await request.post(`${origin}/fixture/action?action=stream`)).ok()).toBe(true);
  await expect(page.getByText("原生大帧后续增量已到达")).toBeVisible();
  const before = await (await request.get(`${origin}/fixture/state`)).json();
  const beforePromotions = promotions;
  expect((await request.post(`${origin}/fixture/action?action=fault`)).ok()).toBe(true);
  await expect(page.getByText("原生大帧最终回复已完成")).toBeVisible({ timeout: 30_000 });
  await expect(page.locator(".readonly-banner")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "重新连接" })).toHaveCount(0);
  await expect(page.getByLabel("发送消息")).toHaveValue("自动恢复必须保留的草稿");
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  const after = await (await request.get(`${origin}/fixture/state`)).json();
  expect(after.attaches).toBe(before.attaches + 1);
  expect(after.native.loads).toBe(before.native.loads + 1);
  expect(after.native.connections).toBe(before.native.connections + 1);
  expect(after.native.faults).toBe(1);
  expect(after.native.unexpected).toBe(0);
  expect(promotions).toBe(beforePromotions);
  expect(mutations).toBe(0);
  expect(sockets).toBe(1);
  expect(navigations).toBe(1);
});
