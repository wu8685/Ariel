import type { Page } from "@playwright/test";

export async function chooseFileView(page: Page, name: "原版模式" | "阅读模式" | "查看 Diff") {
  await page.getByRole("button", { name: "文件菜单", exact: true }).click();
  await page.getByRole("button", { name, exact: true }).click();
}
