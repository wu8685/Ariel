import { expect, test } from "@playwright/test";

test.use({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, isMobile: false, hasTouch: false });

test("desktop login uses the formal color wind-messenger hero and micro navigation mark", async ({ page }) => {
  await page.goto("/");
  const hero = page.locator(".connect-logo.ariel-logo--color");
  const micro = page.locator(".masthead .brand-logo.ariel-logo--micro");
  await expect(hero).toBeVisible();
  await expect(hero.locator("img")).toHaveAttribute("src", "/brand/ariel-logo-wind-messenger-color.png");
  await expect(micro).toBeVisible();
  await expect(micro.locator("img")).toHaveAttribute("src", "/brand/ariel-logo-wind-messenger-micro-white.svg");
  const heroBox = await hero.boundingBox();
  expect(heroBox?.width || 0).toBe(320);
  expect(heroBox?.height || 0).toBe(320);
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute("href", "/brand/ariel-logo-wind-messenger-micro.svg");
  await expect(page.locator('link[rel="apple-touch-icon"]')).toHaveAttribute("href", "/brand/ariel-logo-wind-messenger-color-512.png");
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute("href", "/manifest.webmanifest");
  await expect(page).toHaveScreenshot("desktop-brand-login.png", { animations: "disabled", caret: "hide" });
});

test("all critical logo sizes remain legible on approved backgrounds", async ({ page }) => {
  await page.goto("/");
  await page.evaluate(() => {
    document.body.innerHTML = `<main class="matrix" aria-label="Ariel Logo 关键尺寸矩阵">
      <h1>Ariel · Wind Messenger</h1>
      <section class="row light"><h2>Light / GitHub README</h2>${[16, 24, 32, 64].map(size => `<figure><img src="/brand/ariel-logo-wind-messenger-micro.svg" width="${size}" height="${size}" alt="micro ink ${size}"><figcaption>${size}</figcaption></figure>`).join("")}<figure><img src="/brand/ariel-logo-wind-messenger-ink.png" width="128" height="128" alt="mono ink 128"><figcaption>128</figcaption></figure><figure><img src="/brand/ariel-logo-wind-messenger-ink.png" width="256" height="256" alt="mono ink 256"><figcaption>256</figcaption></figure></section>
      <section class="row dark"><h2>Dark / near black</h2>${[16, 24, 32, 64].map(size => `<figure><img src="/brand/ariel-logo-wind-messenger-micro-white.svg" width="${size}" height="${size}" alt="micro white ${size}"><figcaption>${size}</figcaption></figure>`).join("")}<figure><img src="/brand/ariel-logo-wind-messenger-white.png" width="128" height="128" alt="mono white 128"><figcaption>128</figcaption></figure><figure><img src="/brand/ariel-logo-wind-messenger-white.png" width="256" height="256" alt="mono white 256"><figcaption>256</figcaption></figure></section>
      <section class="row transparent"><h2>Transparent / split canvas</h2><figure><img src="/brand/ariel-logo-wind-messenger-color.png" width="320" height="320" alt="color 320"><figcaption>320</figcaption></figure><figure><img src="/brand/ariel-logo-wind-messenger-color-512.png" width="512" height="512" alt="color 512"><figcaption>512</figcaption></figure></section>
    </main>`;
    const style = document.createElement("style");
    style.textContent = `*{box-sizing:border-box}body{margin:0;background:#eef2f6;color:#162b5c;font:16px -apple-system,BlinkMacSystemFont,sans-serif}.matrix{padding:32px}.matrix>h1{margin:0 0 24px;font:38px Georgia,serif}.row{display:flex;align-items:flex-end;gap:22px;min-height:300px;padding:24px;margin:0 0 24px;border:1px solid #c9d1d9}.row h2{align-self:flex-start;width:190px;margin:0;font-size:16px}.row.light{background:#fff}.row.dark{background:#05070b;color:#f7f9fc;border-color:#30363d}.row.transparent{background:linear-gradient(135deg,#fff 0 50%,#0d1117 50%);color:#246bfd;min-height:570px}figure{display:flex;flex-direction:column;align-items:center;gap:8px;margin:0}img{display:block;object-fit:contain;flex:none}figcaption{font-size:12px}`;
    document.head.append(style);
  });
  await expect(page.getByRole("main", { name: "Ariel Logo 关键尺寸矩阵" })).toBeVisible();
  const images = page.locator(".matrix img");
  await expect(images).toHaveCount(14);
  for (let index = 0; index < 14; index += 1) {
    const image = images.nth(index);
    await expect(image).toHaveJSProperty("complete", true);
    expect(await image.evaluate(node => (node as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    const box = await image.boundingBox();
    expect(Math.abs((box?.width || 0) - (box?.height || 0))).toBeLessThanOrEqual(1);
  }
  await expect(page).toHaveScreenshot("wind-messenger-size-matrix.png", { animations: "disabled", caret: "hide", fullPage: true, maxDiffPixels: 5 });
});
