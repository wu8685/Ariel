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
  await page.getByRole("button", { name: /连接/ }).click();
  await page.getByRole("button", { name: "打开 Puck" }).click({ timeout: 20000 });
  const bot = page.locator(".puck-workspace, .puck-transcript").first();
  await bot.waitFor({ timeout: 20000 });
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
  const bubble = await sample.locator(".puck-bubble, .puck-message-content").first().boundingBox();
  const add = await float.locator(".puck-reaction-add").boundingBox();
  if (!bubble || !add) throw new Error("reaction control has no box");
  const outside = add.x + add.width <= bubble.x + 4 || add.x >= bubble.x + bubble.width - 4;
  if (!outside) throw new Error("reaction add control is inside the bubble");
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
