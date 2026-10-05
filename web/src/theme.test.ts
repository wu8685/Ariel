import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const style = readFileSync(fileURLToPath(new URL("./style.css", import.meta.url)), "utf8");
const interaction = readFileSync(fileURLToPath(new URL("./interaction.css", import.meta.url)), "utf8");
const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8");
const app = readFileSync(fileURLToPath(new URL("./App.tsx", import.meta.url)), "utf8");

function token(name: string): string {
  return style.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})\\s*;`))?.[1]?.toLowerCase() || "";
}

function luminance(hex: string): number {
  const channels = hex.match(/[0-9a-f]{2}/gi)?.map(value => {
    const srgb = parseInt(value, 16) / 255;
    return srgb <= 0.04045 ? srgb / 12.92 : ((srgb + 0.055) / 1.055) ** 2.4;
  }) || [];
  return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
}

function contrast(a: string, b: string): number {
  const values = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (values[0] + 0.05) / (values[1] + 0.05);
}

describe("fixed Night appearance", () => {
  it("uses dark page/surface tokens and high-contrast white text", () => {
    expect(style).toMatch(/color-scheme:\s*dark/);
    expect(token("color-page")).toBe("#000000");
    expect(token("color-surface")).toBe("#0d0d0d");
    expect(token("color-text")).toBe("#ffffff");
    expect(contrast(token("color-text"), token("color-page"))).toBeGreaterThanOrEqual(4.5);
    expect(contrast(token("color-muted"), token("color-surface"))).toBeGreaterThanOrEqual(7);
    expect(contrast(token("color-subtle"), token("color-surface"))).toBeGreaterThanOrEqual(4.5);
    expect(html).toContain('name="theme-color" content="#000000"');
  });

  it("uses blue for primary and keyboard focus while preserving semantic warnings", () => {
    expect(token("color-accent")).toBe("#6fa9ff");
    expect(contrast(token("color-on-primary"), token("color-primary"))).toBeGreaterThanOrEqual(4.5);
    expect(style).toMatch(/\.primary\s*\{[^}]*background:\s*var\(--color-primary\)/s);
    expect(style).toMatch(/:focus-visible\s*\{[^}]*var\(--color-accent\)/s);
    expect(style).toMatch(/\.permission-strip\.danger\s*\{[^}]*var\(--color-warning/s);
    expect(style).toMatch(/\.primary:disabled\s*\{[^}]*background:\s*var\(--color-raised\);[^}]*color:\s*var\(--color-subtle\);[^}]*opacity:\s*1/s);
  });

  it("keeps connection, conversation, composer and question controls on dark surfaces", () => {
    for (const selector of [".connect-panel", ".sidebar", ".conversation", ".composer", ".interaction-card"]) {
      expect(style, selector).toMatch(new RegExp(`${selector.replace(".", "\\.")}\\s*\\{[^}]*background:\\s*var\\(--color-`, "s"));
    }
    expect(interaction).toMatch(/\.question input\s*\{[^}]*background:\s*var\(--color-/s);
    expect(style).toMatch(/@media\s*\(max-width:\s*800px\)/);
    expect(style).toMatch(/prefers-reduced-motion/);
  });

  it("uses a neutral gray logo and a mobile-only dismissible sidebar backdrop", () => {
    expect(token("color-logo")).toBe("#b8b8b8");
    expect(style).toMatch(/\.brand-mark\s*\{[^}]*color:\s*var\(--color-logo\)/s);
    expect(style).toMatch(/\.brand-mark\s*\{[^}]*stroke:\s*currentColor/s);
    expect(style).toMatch(/\.sidebar-backdrop\s*\{[^}]*display:\s*none/s);
    expect(style).toMatch(/@media\s*\(max-width:\s*800px\)[\s\S]*\.sidebar-backdrop\s*\{[^}]*display:\s*block/s);
    expect(app).toMatch(/aria-label="关闭会话列表遮罩"[^>]*onClick=\{\(\) => setShowList\(false\)\}/);
    expect(app).toMatch(/aria-expanded=\{showList\}/);
    expect(app).toMatch(/key === "Escape"[^}]*setShowList\(false\)/);
  });

  it("compacts only connected mobile conversation chrome and keeps touch targets", () => {
    const mobile = style.split(/@media\s*\(max-width:\s*800px\)/)[1];
    expect(mobile).toMatch(/\.app-shell\.connected\s+\.masthead\s*\{[^}]*display:\s*none/s);
    expect(mobile).toMatch(/\.conversation-head\s*\{[^}]*height:\s*6[4-8]px/s);
    expect(mobile).toMatch(/\.mobile-list\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(mobile).toMatch(/\.permission-info-button\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(mobile).toMatch(/\.sidebar-disconnect\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(mobile).toMatch(/\.permission-strip\s*\{[^}]*display:\s*none/s);
    expect(mobile).toMatch(/\.composer\s*\{[^}]*border-radius:\s*\d+px/s);
    expect(mobile).toMatch(/\.composer textarea\s*\{[^}]*max-height:[^}]*overflow-y:\s*auto/s);
    expect(mobile).toMatch(/\.composer textarea\s*\{[^}]*max-height:\s*212px/s);
    expect(mobile).not.toMatch(/\.composer textarea\s*\{[^}]*field-sizing:\s*content/s);
    expect(mobile).toMatch(/\.send-button[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(mobile).toMatch(/safe-area-inset-bottom/);
  });

  it("does not let mobile input focus magnify the composer past the screen edge", () => {
    const mobile = style.split(/@media\s*\(max-width:\s*800px\)/)[1];
    const textarea = mobile.match(/\.composer textarea\s*\{([^}]*)\}/s)?.[1] || "";
    const fontSize = Number(textarea.match(/font-size:\s*(\d+)px/)?.[1]);
    expect(fontSize).toBeGreaterThanOrEqual(16);
  });

  it("keeps mobile approval and question fields from magnifying the page on focus", () => {
    const mobile = style.split(/@media\s*\(max-width:\s*800px\)/)[1];
    expect(mobile).toMatch(/\.question input,\s*\.question select\s*\{[^}]*font-size:\s*16px;[^}]*min-width:\s*0/s);
  });

  it("visually distinguishes an unavailable mobile send button from the blue active button", () => {
    const mobile = style.split(/@media\s*\(max-width:\s*800px\)/)[1];
    expect(mobile).toMatch(/\.send-button:disabled\s*\{[^}]*background:\s*#[0-9a-fA-F]{6};[^}]*border-color:\s*#[0-9a-fA-F]{6};[^}]*color:\s*var\(--color-muted\);[^}]*opacity:\s*1/s);
  });

  it("wraps long messages and approval prompts without horizontal scrolling", () => {
    expect(style).toMatch(/\.message-text\s*\{[^}]*overflow-wrap:\s*anywhere/s);
    expect(style).toMatch(/\.interaction-card p\s*\{[^}]*overflow-wrap:\s*anywhere/s);
    expect(style).toMatch(/\.question\s*\{[^}]*overflow-wrap:\s*anywhere/s);
  });

  it("keeps blue for the user bubble and purposeful highlights, with neutral Codex and system text", () => {
    expect(contrast(token("color-on-primary"), token("color-primary"))).toBeGreaterThanOrEqual(4.5);
    expect(contrast(token("color-text"), token("color-panel"))).toBeGreaterThanOrEqual(4.5);
    expect(style).toMatch(/\.message\.user\s+\.message-body\s*\{[^}]*background:\s*var\(--color-primary\)/s);
    expect(style).toMatch(/\.message\.assistant\s+\.message-body\s*\{[^}]*background:\s*var\(--color-panel\)/s);
    expect(style).toMatch(/\.message\.user\s*\{[^}]*justify-content:\s*flex-end/s);
    expect(style).toMatch(/\.eyebrow\s*\{[^}]*color:\s*var\(--color-subtle\)/s);
    expect(style).toMatch(/\.empty-symbol\s*\{[^}]*color:\s*var\(--color-logo\)/s);
    for (const name of ["color-surface", "color-panel", "color-raised", "color-border"]) {
      const value = token(name).slice(1);
      expect(value.slice(0, 2)).toBe(value.slice(2, 4));
      expect(value.slice(2, 4)).toBe(value.slice(4, 6));
    }
    expect(style).toMatch(/\.activity-toggle\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(style).toMatch(/\.activity-toggle\s*\{[^}]*color:\s*var\(--color-text\)/s);
  });

  it("contains Markdown tables and code inside bubbles with readable links", () => {
    expect(style).toMatch(/\.markdown-table-scroll\s*\{[^}]*overflow-x:\s*auto/s);
    expect(style).toMatch(/\.markdown-body pre\s*\{[^}]*overflow-x:\s*auto/s);
    expect(style).toMatch(/\.markdown-body a\s*\{[^}]*text-decoration:\s*underline/s);
    expect(style).toMatch(/\.markdown-body\s*\{[^}]*overflow-wrap:\s*anywhere/s);
    expect(style).toMatch(/\.markdown-body p\s*\{[^}]*white-space:\s*pre-wrap/s);
    expect(style).toMatch(/\.markdown-body \.sr-only\s*\{[^}]*position:\s*absolute/s);
  });
});
