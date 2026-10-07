import { expect, test } from "@playwright/test";

test.use({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, isMobile: false, hasTouch: false });

test("desktop login uses the formal medium and micro Ariel marks", async ({ page }) => {
  await page.goto("/");
  const medium = page.locator(".connect-logo.ariel-logo--mono");
  const micro = page.locator(".masthead .brand-logo.ariel-logo--micro");
  await expect(medium).toBeVisible();
  await expect(medium.locator("img")).toHaveAttribute("src", "/brand/ariel-logo-mythic-white.png");
  await expect(micro).toBeVisible();
  await expect(micro).toHaveCSS("color", "rgb(184, 184, 184)");
  const mediumBox = await medium.boundingBox();
  expect(mediumBox?.width || 0).toBeGreaterThanOrEqual(128);
  expect(mediumBox?.width || 0).toBeLessThan(320);
  expect(Math.abs((mediumBox?.width || 0) - (mediumBox?.height || 0))).toBeLessThanOrEqual(1);
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute("href", "/brand/ariel-logo-micro.svg");
  await expect(page).toHaveScreenshot("desktop-brand-login.png", { animations: "disabled", caret: "hide" });
});
