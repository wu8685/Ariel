import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { expect, test } from "@playwright/test";

test.use({ viewport: { width: 1440, height: 1000 }, deviceScaleFactor: 1, isMobile: false, hasTouch: false });

test("repository brand guideline renders every referenced wind-messenger asset", async ({ page }) => {
  const guideline = pathToFileURL(resolve(process.cwd(), "../docs/brand/logo-guideline.html")).href;
  await page.goto(guideline);
  await expect(page).toHaveTitle("Ariel · 风之信使 Logo 系统");
  await expect(page.getByRole("heading", { level: 1, name: "风之信使" })).toBeVisible();
  const images = page.locator("img");
  expect(await images.count()).toBeGreaterThanOrEqual(14);
  for (let index = 0; index < await images.count(); index += 1) {
    const image = images.nth(index);
    await expect(image).toHaveJSProperty("complete", true);
    expect(await image.evaluate(node => (node as HTMLImageElement).naturalWidth), `image ${index}`).toBeGreaterThan(0);
  }
  await expect(page).toHaveScreenshot("wind-messenger-guideline.png", { animations: "disabled", caret: "hide", fullPage: true });
});
