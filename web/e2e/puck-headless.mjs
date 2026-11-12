import { chromium } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import path from "node:path";

const origin = process.env.PUCK_E2E_ORIGIN || "";
const pin = process.env.PUCK_E2E_PIN || "";
const profile = process.env.PUCK_E2E_PROFILE || "";
const shots = process.env.PUCK_E2E_SHOTS || "";
if (!origin || !/^[0-9]{6}$/.test(pin) || !profile || !shots) {
  console.error("headless UI check is missing its isolated relay settings");
  process.exit(2);
}
await mkdir(shots, { recursive: true });
await mkdir(profile, { recursive: true });

const context = await chromium.launchPersistentContext(profile, {
  headless: true,
  viewport: { width: 1280, height: 800 },
  locale: "zh-CN",
});
const page = context.pages()[0] || await context.newPage();
const notes = [];
try {
  await page.goto(origin + "/", { waitUntil: "domcontentloaded" });
  await page.locator("#token").fill(pin);
  await page.getByRole("button", { name: "连接", exact: true }).click();
  const openList = page.getByRole("button", { name: /^打开会话列表/ });
  if (await openList.count()) await openList.click();
  const puck = page.locator('button[aria-label="打开 Puck"]');
  try {
    await puck.click({ timeout: 20000 });
  } catch (error) {
    const labels = await page.locator("button").evaluateAll(nodes => nodes.slice(0, 24).map(node => node.getAttribute("aria-label") || (node.textContent || "").trim()));
    throw new Error(`打开 Puck was not clickable; buttons=${labels.join(" | ")}`);
  }
  const bot = page.locator(".puck-workspace, .puck-transcript").first();
  await bot.waitFor({ timeout: 20000 });
  const named = page.locator(".puck-assistant-row", { hasText: "E2E" }).first();
  if (await named.count()) await named.click();
  const messages = page.locator(".puck-message");
  const count = await messages.count();
  if (count < 1) throw new Error("Puck chat did not render a message");
  notes.push(`messages=${count}`);
  const sample = messages.first();
  const emptyRow = await sample.locator(".puck-reactions").count();
  if (emptyRow !== 0) throw new Error("reaction row is present before any reaction");
  await sample.hover();
  const float = sample.locator(".puck-reaction-float");
  await float.waitFor({ timeout: 5000 });
  const bubble = await sample.locator(".puck-message-content").first().boundingBox();
  const add = await float.locator(".puck-reaction-add").boundingBox();
  if (!bubble || !add) throw new Error("reaction control has no box");
  const sticksOut = add.x < bubble.x - 2 || add.x + add.width > bubble.x + bubble.width + 2;
  if (!sticksOut) throw new Error("reaction add control does not sit outside the bubble");
  notes.push("reaction-float=outside");
  const tasks = page.locator(".puck-task-card, [aria-label='任务卡片']");
  const taskCount = await tasks.count();
  notes.push(`taskCards=${taskCount}`);
  if (taskCount > 0) await tasks.first().scrollIntoViewIfNeeded();
  await page.screenshot({ path: path.join(shots, "puck-chat.png"), fullPage: true });
  console.log(notes.join(" "));
} catch (error) {
  await page.locator("#token").fill("").catch(() => {});
  await page.screenshot({ path: path.join(shots, "puck-chat-failed.png"), fullPage: true }).catch(() => {});
  console.error(error instanceof Error ? error.message : "ui check failed");
  process.exit(1);
} finally {
  await context.close();
}
