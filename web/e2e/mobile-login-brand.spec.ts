import { expect, test } from "@playwright/test";

test("mobile login shows the color hero without horizontal overflow and keeps the PIN form reachable", async ({ page }) => {
  await page.goto("/");
  const hero = page.locator(".connect-logo.ariel-logo--color");
  await expect(hero).toBeVisible();
  await expect(hero.locator("img")).toHaveAttribute("src", "/brand/ariel-logo-wind-messenger-color.png");
  const box = await hero.boundingBox();
  expect(box?.width || 0).toBe(128);
  expect(box?.height || 0).toBe(128);
  const title = page.getByRole("heading", { name: "接续 Codex", exact: true });
  const titleBox = await title.boundingBox();
  await expect(title).toHaveCSS("white-space", "nowrap");
  expect((box?.x || 0) + (box?.width || 0)).toBeLessThan(titleBox?.x || 0);
  expect(titleBox?.y || 0).toBeLessThan((box?.y || 0) + (box?.height || 0));
  expect((titleBox?.y || 0) + (titleBox?.height || 0)).toBeGreaterThan(box?.y || 0);
  const geometry = await page.evaluate(() => ({ viewportWidth: window.innerWidth, bodyWidth: document.body.scrollWidth }));
  expect(geometry.bodyWidth).toBeLessThanOrEqual(geometry.viewportWidth);
  const pin = page.getByLabel("6 位连接码");
  await pin.scrollIntoViewIfNeeded();
  await expect(pin).toBeVisible();
  await expect(page.getByRole("button", { name: "连接", exact: true })).toBeVisible();
  await expect(page).toHaveScreenshot("mobile-brand-login-color.png", { animations: "disabled", caret: "hide", fullPage: true });
});
