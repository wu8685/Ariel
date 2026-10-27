import { expect, test } from "@playwright/test";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const origin = "http://127.0.0.1:4182";
let scratch = "";
let fixture: ChildProcess | null = null;
let fixtureOutput = "";

test.beforeAll(async () => {
  scratch = mkdtempSync(join(tmpdir(), "ariel-native-recovery-e2e-"));
  const binary = join(scratch, "cache-recovery");
  const build = spawnSync("go", ["build", "-race", "-o", binary, "./tests/fixtures/cache-recovery"], { cwd: resolve(webRoot, ".."), stdio: "pipe" });
  if (build.status !== 0) throw new Error(build.stderr.toString());
  fixture = spawn(binary, ["-dist", join(webRoot, "dist"), "-stream-interval", "0"], { stdio: "pipe" });
  fixture.stderr?.on("data", chunk => { fixtureOutput += String(chunk); });
  for (let attempt = 0; attempt < 80; attempt++) {
    try {
      if ((await fetch(`${origin}/fixture/state`)).ok) return;
    } catch { /* process is still starting */ }
    if (fixture.exitCode !== null) throw new Error(fixtureOutput);
    await new Promise(resolveWait => setTimeout(resolveWait, 100));
  }
  throw new Error(`native Agent fixture did not start: ${fixtureOutput}`);
});

test.afterAll(async () => {
  fixture?.kill("SIGTERM");
  if (fixture && fixture.exitCode === null) await new Promise(resolveExit => fixture?.once("exit", resolveExit));
  if (scratch) rmSync(scratch, { recursive: true, force: true });
  expect(fixtureOutput).not.toContain("DATA RACE");
});

test("real Agent keeps a terminal snapshot after late refresh failure and recovers a dead native follower without reload", async ({ page, request }) => {
  const action = async (name: string) => {
    const response = await request.post(`${origin}/fixture/action?action=${name}`);
    expect(response.ok()).toBe(true);
  };
  await page.goto(origin);
  await page.getByLabel("6 位连接码").fill("012345");
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await page.getByRole("button", { name: "展开项目 native-cache-recovery，/tmp/native-cache-recovery" }).click();
  await page.getByText("真实 Agent 恢复验收").click();
  await expect(page.getByText("原生会话上一轮已完成")).toBeVisible();
  await page.getByLabel("发送消息").fill("恢复期间必须保留的草稿");

  await action("continue-blocked");
  await expect(page.getByText("原生会话仍在处理")).toBeVisible();
  await expect.poll(async () => (await (await request.get(`${origin}/fixture/state`)).json()).refreshes).toBe(1);
  await action("complete");
  await expect(page.getByText("原生会话最终回复自动到达")).toBeVisible();
  await action("release");
  await expect.poll(async () => (await (await request.get(`${origin}/fixture/state`)).json()).finished).toBe(1);
  await expect(page.locator(".readonly-banner")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();

  await action("continue");
  await expect(page.getByText("原生会话仍在处理")).toBeVisible();
  await action("disconnect");
  await expect(page.locator(".readonly-banner")).toBeVisible();
  await expect(page.getByText("实时连接暂时失效，请重新连接；历史内容已保留。")).toBeVisible();
  await expect(page.locator(".send-button")).toBeDisabled();
  await action("complete");
  await page.getByRole("button", { name: "重新连接" }).click();
  await expect(page.getByText("原生会话最终回复自动到达")).toBeVisible();
  await expect(page.locator(".runtime")).toHaveText("待命");
  await expect(page.locator(".readonly-banner")).toHaveCount(0);
  await expect(page.getByLabel("发送消息")).toHaveValue("恢复期间必须保留的草稿");
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  expect((await (await request.get(`${origin}/fixture/state`)).json()).attaches).toBe(2);
});

test("native HTML characters do not force a reconnect through the real Agent and Relay", async ({ page, request }) => {
  const action = async (name: string) => expect((await request.post(`${origin}/fixture/action?action=${name}`)).ok()).toBe(true);
  await action("continue");
  await action("complete");
  await page.goto(origin);
  await page.getByLabel("6 位连接码").fill("012345");
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await page.getByRole("button", { name: "展开项目 native-cache-recovery，/tmp/native-cache-recovery" }).click();
  await page.getByText("真实 Agent 恢复验收").click();
  await expect(page.getByText("原生会话最终回复自动到达")).toBeVisible();
  const before = await (await request.get(`${origin}/fixture/state`)).json();
  await page.getByLabel("发送消息").fill("编码修复不能丢失的草稿");
  await action("special-json");
  const reply = page.locator(".message-text").filter({ hasText: "HTML 字符状态正常" });
  await expect(reply).toBeVisible();
  await expect(reply).toContainText("<>&");
  await expect(page.locator(".readonly-banner")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "重新连接" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
  await expect(page.getByLabel("发送消息")).toHaveValue("编码修复不能丢失的草稿");
  expect((await (await request.get(`${origin}/fixture/state`)).json()).attaches).toBe(before.attaches);
});

test("production streaming cadence coalesces text for 5s but completes immediately", async ({ page, request }) => {
  const action = async (name: string) => expect((await request.post(`${origin}/fixture/action?action=${name}`)).ok()).toBe(true);
  // Also runnable by --grep on its own; do not depend on the previous case.
  await action("continue");
  await action("complete");
  await page.goto(origin);
  await page.getByLabel("6 位连接码").fill("012345");
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await page.getByRole("button", { name: "展开项目 native-cache-recovery，/tmp/native-cache-recovery" }).click();
  await page.getByText("真实 Agent 恢复验收").click();
  await expect(page.getByText("原生会话最终回复自动到达")).toBeVisible();
  await page.getByLabel("发送消息").fill("streaming 期间保留的草稿");
  await action("continue");
  await expect(page.getByText("原生会话仍在处理")).toBeVisible();
  const began = Date.now();
  await action("stream");
  await expect.poll(async () => (await (await request.get(`${origin}/fixture/state`)).json()).observedStreamRevision).toBe(1);
  // Inspect an actual interval, rather than polling until eventual success.
  await page.waitForTimeout(1400);
  await expect(page.getByText("原生会话仍在处理")).toBeVisible();
  await expect(page.getByText("原生增量正文 1")).toHaveCount(0);
  await action("stream");
  await expect.poll(async () => (await (await request.get(`${origin}/fixture/state`)).json()).observedStreamRevision).toBe(2);
  await expect(page.getByText("原生增量正文 2")).toBeVisible({ timeout: 6000 });
  expect(Date.now() - began).toBeGreaterThan(4000);
  expect(Date.now() - began).toBeLessThan(6500);
  await action("stream");
  await expect.poll(async () => (await (await request.get(`${origin}/fixture/state`)).json()).observedStreamRevision).toBe(3);
  await action("complete");
  await expect(page.getByText("原生会话最终回复自动到达")).toBeVisible({ timeout: 2000 });
  await expect(page.getByLabel("发送消息")).toHaveValue("streaming 期间保留的草稿");
  await expect(page.getByRole("button", { name: "发送", exact: true })).toBeEnabled();
});

test("native retry notices coalesce and recover through the real Agent and Relay without losing the draft", async ({ page, request }) => {
  const action = async (name: string) => expect((await request.post(`${origin}/fixture/action?action=${name}`)).ok()).toBe(true);
  await action("continue");
  await action("complete");
  await page.goto(origin);
  await page.getByLabel("6 位连接码").fill("012345");
  await page.getByRole("button", { name: "连接", exact: true }).click();
  await page.getByRole("button", { name: "展开项目 native-cache-recovery，/tmp/native-cache-recovery" }).click();
  await page.getByText("真实 Agent 恢复验收").click();
  await expect(page.getByText("原生会话最终回复自动到达")).toBeVisible();
  const draft = page.getByLabel("发送消息");
  await draft.fill("重连期间保留的草稿");
  await action("retry");
  await expect(page.locator(".native-notice.retry")).toContainText("Reconnecting... 1/5");
  await action("retry-again");
  const retry = page.locator(".native-notice.retry");
  await expect(retry).toHaveCount(1);
  await expect(retry).toContainText("Reconnecting... waiting for network");
  await expect(page.getByText("[error 项目]", { exact: true })).toHaveCount(0);
  await expect(page.locator(".agent-progress")).toHaveCount(0);
  const box = await retry.boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390);
  const retryColor = await retry.evaluate(el => getComputedStyle(el).color);
  await expect(page.locator(".turn")).toHaveScreenshot("native-retrying.png");
  await action("retry-resume");
  await expect(page.getByText("网络恢复后继续回复")).toBeVisible();
  await expect(retry).toHaveCount(0);
  await action("retry-fail");
  const failure = page.locator(".native-notice.failure");
  await expect(failure).toContainText("Request failed after retries");
  await expect(failure).toHaveAttribute("role", "alert");
  expect(await failure.evaluate(el => getComputedStyle(el).color)).not.toBe(retryColor);
  await expect(retry).toHaveCount(0);
  await expect(page.locator(".turn")).toHaveScreenshot("native-error.png");
  await action("retry-unknown");
  await expect(page.locator(".native-notice.unknown")).toContainText("Unclassified native error");
  await expect(draft).toHaveValue("重连期间保留的草稿");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await action("continue");
  await action("complete");
});
