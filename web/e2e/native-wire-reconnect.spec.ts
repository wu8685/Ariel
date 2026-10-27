import { expect, test } from "@playwright/test";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const origin = "http://127.0.0.1:4183";
let scratch = "";
let fixture: ChildProcess | null = null;
let output = "";

test.beforeAll(async () => {
  scratch = mkdtempSync(join(tmpdir(), "ariel-native-wire-e2e-"));
  // Allows the identical browser assertions to run against an isolated old
  // source build for Red evidence; it never switches the working tree.
  let binary = process.env.ARIEL_NATIVE_WIRE_FIXTURE_BINARY;
  if (!binary) {
    binary = join(scratch, "cache-recovery");
    const build = spawnSync("go", ["build", "-race", "-o", binary, "./tests/fixtures/cache-recovery"], { cwd: resolve(webRoot, ".."), stdio: "pipe" });
    if (build.status !== 0) throw new Error(build.stderr.toString());
  }
  fixture = spawn(binary, ["-listen", "127.0.0.1:4183", "-dist", join(webRoot, "dist"), "-native-wire"], { stdio: "pipe" });
  fixture.stderr?.on("data", chunk => { output += String(chunk); });
  for (let attempt = 0; attempt < 80; attempt++) {
    try { if ((await fetch(`${origin}/fixture/state`)).ok) return; } catch { /* starting */ }
    if (fixture.exitCode !== null) throw new Error(output);
    await new Promise(done => setTimeout(done, 100));
  }
  throw new Error(`native wire fixture did not start: ${output}`);
});

test.afterAll(async () => {
  fixture?.kill("SIGTERM");
  if (fixture && fixture.exitCode === null) await new Promise(done => fixture?.once("exit", done));
  if (scratch) rmSync(scratch, { recursive: true, force: true });
  expect(output).not.toContain("DATA RACE");
});

test.afterEach(async ({}, info) => {
  if (info.status !== info.expectedStatus) {
    await info.attach("isolated-agent-log", { body: output, contentType: "text/plain" });
  }
});

test("production-size native IPC opens and pushes terminal text through Agent and Relay without reconnecting", async ({ page, request }) => {
  test.setTimeout(30_000);
  let navigations = 0;
  page.on("framenavigated", frame => { if (frame === page.mainFrame()) navigations++; });
  const state = async () => (await request.get(`${origin}/fixture/state`)).json();
  await page.goto(origin);
  await page.getByLabel("6 位连接码").fill("012345");
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await page.getByRole("button", { name: "展开项目 native-wire-regression，/tmp/native-wire-regression" }).click();
  await page.getByText("原生大帧回归").click();
  await expect(page.getByText("原生大帧首次打开成功")).toBeVisible({ timeout: 12_000 });
  const initial = await state();
  expect(initial.native.wireBytes).toBeGreaterThan(8 * 1024 * 1024);
  expect(initial.native.wireBytes).toBeLessThan(64 * 1024 * 1024);
  expect(initial.residentBytes).toBeGreaterThan(1.5 * 1024 * 1024);
  expect(initial.residentBytes).toBeLessThan(2 * 1024 * 1024);
  expect(initial.native.connections).toBe(2); // ordinary → bounded large fallback
  expect(initial.native.loads).toBe(2);
  expect(initial.attaches).toBe(1);
  const draft = page.getByLabel("发送消息");
  await draft.fill("大帧更新期间必须保留的草稿");
  for (const [action, text] of [["stream", "原生大帧后续增量已到达"], ["complete", "原生大帧最终回复已完成"]]) {
    expect((await request.post(`${origin}/fixture/action?action=${action}`)).ok()).toBe(true);
    await expect(page.getByText(text)).toBeVisible();
    await expect(page.locator(".readonly-banner")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "重新连接" })).toHaveCount(0);
    await expect(draft).toHaveValue("大帧更新期间必须保留的草稿");
  }
  await expect(page.locator(".runtime")).toHaveText("待命");
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  await expect(page.getByText("正在同步会话...", { exact: true })).toHaveCount(0);
  const final = await state();
  expect(final.attaches).toBe(1);
  expect(final.native.loads).toBe(initial.native.loads);
  expect(final.native.connections).toBe(initial.native.connections);
  expect(final.native.patches).toBe(2);
  expect(final.native.unexpected).toBe(0);
  expect(navigations).toBe(1);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
