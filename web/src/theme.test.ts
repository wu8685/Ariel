import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const style = readFileSync(fileURLToPath(new URL("./style.css", import.meta.url)), "utf8");
const interaction = readFileSync(fileURLToPath(new URL("./interaction.css", import.meta.url)), "utf8");
const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8");
const app = readFileSync(fileURLToPath(new URL("./App.tsx", import.meta.url)), "utf8");
const main = readFileSync(fileURLToPath(new URL("./main.tsx", import.meta.url)), "utf8");

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

  it("uses the formal image assets without CSS recoloring and a cross-device dismissible sidebar backdrop", () => {
    expect(style).toMatch(/\.ariel-logo\s+img\s*\{[^}]*object-fit:\s*contain/s);
    expect(style).not.toMatch(/\.ariel-logo[^}]*mask:/s);
    expect(style).not.toMatch(/\.ariel-logo[^}]*filter:/s);
    expect(html).toContain('rel="icon" type="image/svg+xml" href="/brand/ariel-logo-wind-messenger-micro.svg"');
    expect(style).toMatch(/\.sidebar-backdrop\s*\{[^}]*display:\s*block/s);
    expect(app).toMatch(/aria-label="关闭会话列表遮罩"[^>]*onClick=\{\(\) => setShowList\(false\)\}/);
    expect(app).toMatch(/aria-expanded=\{showList\}/);
    expect(app).toMatch(/key === "Escape"[^}]*setShowList\(false\)/);
  });

  it("fully collapses the mobile conversation header while keeping expanded touch targets", () => {
    const mobile = style.split(/@media\s*\(max-width:\s*800px\)/)[1];
    expect(mobile).toMatch(/\.app-shell\.connected\s+\.masthead\s*\{[^}]*display:\s*none/s);
    expect(mobile).toMatch(/\.conversation-head\s*\{[^}]*height:\s*6[4-8]px/s);
    expect(mobile).toMatch(/\.conversation-head\.history-collapsed\s*\{[^}]*height:\s*0;[^}]*padding:\s*0\s+12px;[^}]*border-bottom-width:\s*0;[^}]*border-bottom-color:\s*transparent/s);
    expect(style).toMatch(/\.conversation-head\.history-collapsed\s*\{[^}]*visibility:\s*hidden;[^}]*pointer-events:\s*none;[^}]*overflow:\s*hidden/s);
    expect(style).toMatch(/\.composer-wrap\s*\{[^}]*display:\s*grid;[^}]*grid-template-rows:\s*minmax\(0,\s*1fr\)/s);
    expect(style).toMatch(/\.composer-wrap\.history-collapsed\s*\{[^}]*grid-template-rows:\s*minmax\(0,\s*0fr\);[^}]*padding-top:\s*0;[^}]*padding-bottom:\s*0;[^}]*visibility:\s*hidden;[^}]*pointer-events:\s*none/s);
    expect(style).toMatch(/\.return-latest-bar\.history-overlay\s*\{[^}]*position:\s*absolute;[^}]*bottom:\s*max\([^}]*safe-area-inset-bottom/s);
    expect(style).toMatch(/\.return-latest-bar button\s*\{[^}]*display:\s*grid;[^}]*place-items:\s*center;[^}]*width:\s*40px;[^}]*height:\s*40px;[^}]*padding:\s*0;/s);
    expect(style).toMatch(/\.return-latest-bar button svg\s*\{[^}]*width:\s*18px;[^}]*height:\s*18px/s);
    expect(mobile).toMatch(/\.mobile-list\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(mobile).toMatch(/\.permission-info-button\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(mobile).toMatch(/\.sidebar-disconnect\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(mobile).toMatch(/\.permission-strip\s*\{[^}]*display:\s*none/s);
    expect(mobile).toMatch(/\.composer\s*\{[^}]*border-radius:\s*var\(--radius-work-item\)/s);
    expect(mobile).toMatch(/\.composer\s*\{[^}]*flex-direction:\s*column;[^}]*align-items:\s*stretch/s);
    expect(mobile).toMatch(/\.composer textarea\s*\{[^}]*max-height:[^}]*overflow-y:\s*auto/s);
    expect(mobile).toMatch(/\.composer textarea\s*\{[^}]*max-height:\s*212px/s);
    expect(mobile).toMatch(/\.composer textarea\s*\{[^}]*width:\s*100%/s);
    expect(mobile).not.toMatch(/\.composer textarea\s*\{[^}]*field-sizing:\s*content/s);
    expect(mobile).toMatch(/\.composer-actions\s*\{[^}]*width:\s*100%/s);
    expect(mobile).toMatch(/\.composer-actions\s*>\s*div\s*\{[^}]*width:\s*100%/s);
    expect(mobile).toMatch(/\.attach-button\s*\{[^}]*min-width:\s*36px;[^}]*min-height:\s*36px;[^}]*margin-right:\s*auto/s);
    expect(mobile).toMatch(/\.send-button[^}]*min-width:\s*36px;[^}]*min-height:\s*36px/s);
    expect(mobile).toMatch(/\.stop-button[^}]*min-width:\s*36px;[^}]*min-height:\s*36px/s);
    expect(mobile).toMatch(/\.send-glyph[^}]*font-size:\s*16px/s);
    expect(mobile).toMatch(/\.stop-glyph[^}]*font-size:\s*10px/s);
    expect(mobile).toMatch(/\.stop-label\s*\{[^}]*display:\s*none/s);
    expect(mobile).toMatch(/safe-area-inset-bottom/);
  });

  it("reuses the compact mobile conversation shell on desktop", () => {
    const desktop = style.split(/@media\s*\(max-width:\s*800px\)/)[0];
    expect(desktop).toMatch(/\.app-shell\.connected\s+\.masthead\s*\{[^}]*display:\s*none/s);
    expect(desktop).toMatch(/\.workspace\s*\{[^}]*display:\s*block;[^}]*position:\s*relative/s);
    expect(desktop).toMatch(/\.sidebar\s*\{[^}]*position:\s*absolute;[^}]*transform:\s*translateX\(-101%\)/s);
    expect(desktop).toMatch(/\.sidebar\.open\s*\{[^}]*transform:\s*translateX\(0\)/s);
    expect(desktop).toMatch(/\.mobile-list\s*\{[^}]*display:\s*grid;[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
    expect(desktop).toMatch(/\.conversation-head\s*\{[^}]*height:\s*66px;[^}]*padding:\s*9px\s+12px/s);
    expect(desktop).toMatch(/\.permission-info\s*\{[^}]*display:\s*block/s);
    expect(desktop).toMatch(/\.permission-strip\s*\{[^}]*display:\s*none/s);
    expect(desktop).toMatch(/\.composer\s*\{[^}]*flex-direction:\s*column;[^}]*border-radius:\s*var\(--radius-work-item\)/s);
    expect(desktop).toMatch(/\.composer textarea\s*\{[^}]*min-height:\s*44px;[^}]*font-size:\s*14px/s);
    expect(desktop).toMatch(/\.send-label,\s*\.stop-label\s*\{[^}]*display:\s*none/s);
  });

  it("shares one work-item corner radius between the queue and composer", () => {
    const desktop = style.split(/@media\s*\(max-width:\s*800px\)/)[0];
    const mobile = style.split(/@media\s*\(max-width:\s*800px\)/)[1];
    expect(style).toMatch(/--radius-work-item:\s*16px;/);
    expect(desktop).toMatch(/\.queue-panel\s*\{[^}]*border-radius:\s*var\(--radius-work-item\)/s);
    expect(desktop).toMatch(/\.composer\s*\{[^}]*border-radius:\s*var\(--radius-work-item\)/s);
    expect(mobile).toMatch(/\.queue-panel\s*\{[^}]*border-radius:\s*var\(--radius-work-item\)/s);
    expect(mobile).toMatch(/\.composer\s*\{[^}]*border-radius:\s*var\(--radius-work-item\)/s);
  });

  it("shows labels for icon controls only through hover or keyboard focus tooltips", () => {
    expect(style).toMatch(/\.icon-control\[data-tooltip\]::after\s*\{[^}]*content:\s*attr\(data-tooltip\);[^}]*opacity:\s*0;[^}]*visibility:\s*hidden/s);
    expect(style).toMatch(/@media\s*\(hover:\s*hover\)\s*and\s*\(pointer:\s*fine\)[\s\S]*\.icon-control\[data-tooltip\]:is\(:hover,\s*:focus-visible\)::after\s*\{[^}]*opacity:\s*1;[^}]*visibility:\s*visible/s);
  });

  it("matches the mobile composer font size to conversation text", () => {
    const mobile = style.split(/@media\s*\(max-width:\s*800px\)/)[1];
    const textarea = mobile.match(/\.composer textarea\s*\{([^}]*)\}/s)?.[1] || "";
    const message = style.match(/\.message-text\s*\{([^}]*)\}/s)?.[1] || "";
    expect(textarea).toMatch(/font-size:\s*14px/);
    expect(message).toMatch(/font-size:\s*14px/);
    expect(main).toMatch(/configureIOSInputViewport\(document, navigator\)/);
  });

  it("gives the portaled queue menu an explicit compact type scale and themed buttons", () => {
    const menu = style.match(/\.queue-menu\s*\{([^}]*)\}/s)?.[1] || "";
    const menuButton = style.match(/\.queue-menu button\s*\{([^}]*)\}/s)?.[1] || "";
    expect(menu).toMatch(/width:\s*108px/);
    expect(menu).toMatch(/font-size:\s*12px/);
    expect(menu).toMatch(/line-height:\s*1\.4/);
    expect(menuButton).toMatch(/min-height:\s*34px/);
    expect(menuButton).toMatch(/border:\s*0/);
    expect(menuButton).toMatch(/border-radius:\s*6px/);
    expect(menuButton).toMatch(/background:\s*transparent/);
    expect(menuButton).toMatch(/color:\s*var\(--color-muted\)/);
    expect(menuButton).toMatch(/font-size:\s*inherit/);
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
    expect(app).toMatch(/tone="white"[^>]*session-loading-logo/s);
    for (const name of ["color-surface", "color-panel", "color-raised", "color-border"]) {
      const value = token(name).slice(1);
      expect(value.slice(0, 2)).toBe(value.slice(2, 4));
      expect(value.slice(2, 4)).toBe(value.slice(4, 6));
    }
    expect(style).toMatch(/\.activity-toggle\s*\{[^}]*border:\s*0;[^}]*background:\s*transparent;[^}]*color:\s*var\(--color-subtle\);[^}]*font-size:\s*12px/s);
    expect(style).not.toMatch(/\.activity-toggle\s*\{[^}]*min-width:\s*44px;[^}]*min-height:\s*44px/s);
  });

  it("contains Markdown tables and code inside bubbles with readable links", () => {
    expect(style).toMatch(/\.markdown-table-scroll\s*\{[^}]*overflow-x:\s*auto;[^}]*font-size:\s*13px;[^}]*line-height:\s*1\.55;[^}]*-webkit-text-size-adjust:\s*100%;[^}]*text-size-adjust:\s*100%/s);
    expect(style).toMatch(/\.markdown-table-scroll table\s*\{[^}]*font:\s*inherit/s);
    expect(style).toMatch(/\.markdown-table-scroll th,\s*\.markdown-table-scroll td\s*\{[^}]*font:\s*inherit/s);
    expect(style).toMatch(/\.markdown-body pre\s*\{[^}]*overflow-x:\s*auto/s);
    expect(style).toMatch(/\.markdown-body a\s*\{[^}]*text-decoration:\s*underline/s);
    expect(style).toMatch(/\.markdown-body\s*\{[^}]*overflow-wrap:\s*anywhere/s);
    expect(style).toMatch(/\.markdown-body p\s*\{[^}]*white-space:\s*pre-wrap/s);
    expect(style).toMatch(/\.markdown-body \.sr-only\s*\{[^}]*position:\s*absolute/s);
  });
});
