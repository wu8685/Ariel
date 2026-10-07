import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(process.cwd(), "..");
const read = (path: string) => readFileSync(resolve(root, path), "utf8");

describe("public Ariel positioning", () => {
  it("defines Ariel as a browser relay for desktop Agent Apps without hiding current support", () => {
    const readme = read("README.md");
    expect(readme).toContain('<p align="center">在浏览器中完成与桌面端 Agent App 的远程会话。</p>');
    expect(readme).toContain("Desktop Agent");
    expect(readme).toContain("Agent App Adapter");
    expect(readme).toContain("Agent App");
    expect(readme).toContain("## 当前支持范围");
    expect(readme).toContain("macOS");
    expect(readme).toContain("Codex Desktop");
    expect(readme).toContain("当前实现矩阵，不是 Ariel 的产品边界");
    expect(readme).not.toContain("在手机浏览器中接续 Mac 上的 Codex Desktop 会话");
  });

  it("keeps every repository-relative README link resolvable", () => {
    const readme = read("README.md");
    const links = [...readme.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)].map(match => match[1].split("#")[0]);
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) expect(existsSync(resolve(root, link)), link).toBe(true);
  });

  it("publishes a provider-neutral architecture while marking unfinished abstraction work", () => {
    const path = "docs/architecture/overview.md";
    expect(existsSync(resolve(root, path))).toBe(true);
    const overview = read(path);
    for (const component of ["Browser", "Relay", "Desktop Agent", "Agent App Adapter", "Agent App"]) expect(overview).toContain(component);
    expect(overview).toContain("当前支持矩阵");
    expect(overview).toContain("macOS");
    expect(overview).toContain("Codex Desktop");
    expect(overview).toContain("尚未完成");
  });

  it("keeps web and installable-app metadata provider neutral", () => {
    const html = read("web/index.html");
    const manifest = read("web/public/manifest.webmanifest");
    expect(html).toContain("Ariel — Agent 联络中继器");
    expect(html).toContain("在浏览器中完成与桌面端 Agent App 的远程会话。");
    expect(manifest).toContain('"name": "Ariel — Agent 联络中继器"');
    expect(manifest).toContain('"description": "在浏览器中完成与桌面端 Agent App 的远程会话。"');
    expect(`${html}\n${manifest}`).not.toMatch(/Codex|Mac 上/);
  });

  it("describes the wind-messenger routes as Agent App session endpoints", () => {
    const brand = `${read("docs/brand/brand-spec.md")}\n${read("docs/brand/logo-guideline.html")}`;
    expect(brand).toContain("Agent App 会话端点");
    expect(brand).not.toMatch(/多个 Codex App|多 Codex App|multiple Codex Apps/);
  });
});
