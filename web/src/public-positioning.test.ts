import { existsSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
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
    expect(readme).toContain("macOS");
    expect(readme).toContain("Codex Desktop");
    expect(readme).toContain("尚未适配");
    expect(readme).not.toContain("在手机浏览器中接续 Mac 上的 Codex Desktop 会话");
  });

  it("keeps every repository-relative README link resolvable", () => {
    const readme = read("README.md");
    const links = [...readme.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)].map(match => match[1].split("#")[0]);
    expect(links.length).toBeGreaterThan(0);
    for (const link of links.filter(link => !/^(https?:|mailto:|#)/.test(link))) expect(existsSync(resolve(root, link)), link).toBe(true);
  });

  it("keeps four concise README sections and moves operational detail into project docs", () => {
    const readme = read("README.md");
    expect([...readme.matchAll(/^## (.+)$/gm)].map(match => match[1])).toEqual(["解决什么问题", "功能", "架构", "安装"]);
    expect(readme.split("\n").length).toBeLessThanOrEqual(70);
    expect(readme).toContain("docs/README.md");
    expect(readme).not.toMatch(/MiB|512 MiB|12 MP|--enable-document-assets|###/);
    expect(existsSync(resolve(root, "docs/README.md"))).toBe(true);
    expect(existsSync(resolve(root, "docs/guide/features.md"))).toBe(true);
    expect(existsSync(resolve(root, "docs/operations/file-context.md"))).toBe(true);
    const guide = read("docs/guide/features.md");
    for (const detail of ["最近两个 turn", "最多三个 turn", "5 秒", "不是 Codex", "停止后", "阅读模式"]) expect(guide).toContain(detail);
    expect(read("docs/operations/chilling-file-context.md")).not.toContain("./bin/chilling serve");
    expect(read("docs/operations/chilling-file-context.md")).toContain("file-context.md");
    expect(read("docs/architecture/overview.md")).toContain("进程内存");
  });

  it("resolves all local links from the documentation entry and maintained guides", () => {
    const paths = ["README.md", "docs/README.md", "docs/guide/features.md", "docs/operations/file-context.md", "docs/operations/chilling-file-context.md", "docs/architecture/overview.md", "docs/operations/agent-install.md", "docs/operations/container-deployment.md"];
    for (const path of paths) {
      expect(existsSync(resolve(root, path)), path).toBe(true);
      const body = read(path);
      for (const match of body.matchAll(/\[[^\]]+\]\(([^)]+)\)/g)) {
        const link = match[1].split("#")[0];
        if (!link || /^(https?:|mailto:)/.test(link)) continue;
        expect(existsSync(resolve(root, path, "..", decodeURIComponent(link))), `${path} → ${link}`).toBe(true);
      }
    }
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

  it("keeps the README and architecture Mermaid diagrams parseable", () => {
    const diagrams: string[] = [];
    for (const path of ["README.md", "docs/architecture/overview.md"]) {
      const blocks = [...read(path).matchAll(/```mermaid\n([\s\S]*?)```/g)];
      expect(blocks.length, path).toBeGreaterThan(0);
      diagrams.push(...blocks.map(block => block[1]));
    }
    // A fresh process gives Mermaid its real DOMPurify export, outside Vitest's SSR loader.
    execFileSync(process.execPath, ["--input-type=module", "-e", `
      import { JSDOM } from "jsdom";
      const dom = new JSDOM("<!doctype html><html><body></body></html>");
      globalThis.window = dom.window;
      globalThis.document = dom.window.document;
      const mermaid = (await import("mermaid")).default;
      mermaid.initialize({ startOnLoad: false, securityLevel: "strict" });
      for (const diagram of JSON.parse(process.argv[1])) await mermaid.parse(diagram);
      dom.window.close();
    `, JSON.stringify(diagrams)], { cwd: resolve(root, "web"), timeout: 10000, stdio: "pipe" });
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
