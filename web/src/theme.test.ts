import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const style = readFileSync(fileURLToPath(new URL("./style.css", import.meta.url)), "utf8");
const interaction = readFileSync(fileURLToPath(new URL("./interaction.css", import.meta.url)), "utf8");
const html = readFileSync(fileURLToPath(new URL("../index.html", import.meta.url)), "utf8");

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
    expect(token("color-page")).toBe("#0b0f16");
    expect(token("color-surface")).toBe("#121923");
    expect(token("color-text")).toBe("#f3f7ff");
    expect(contrast(token("color-text"), token("color-page"))).toBeGreaterThanOrEqual(4.5);
    expect(contrast(token("color-muted"), token("color-surface"))).toBeGreaterThanOrEqual(4.5);
    expect(html).toContain('name="theme-color" content="#0b0f16"');
  });

  it("uses blue for primary and keyboard focus while preserving semantic warnings", () => {
    expect(token("color-accent")).toBe("#60a5fa");
    expect(contrast(token("color-on-primary"), token("color-primary"))).toBeGreaterThanOrEqual(4.5);
    expect(style).toMatch(/\.primary\s*\{[^}]*background:\s*var\(--color-primary\)/s);
    expect(style).toMatch(/:focus-visible\s*\{[^}]*var\(--color-accent\)/s);
    expect(style).toMatch(/\.permission-strip\.danger\s*\{[^}]*var\(--color-warning/s);
  });

  it("keeps connection, conversation, composer and question controls on dark surfaces", () => {
    for (const selector of [".connect-panel", ".sidebar", ".conversation", ".composer", ".interaction-card"]) {
      expect(style, selector).toMatch(new RegExp(`${selector.replace(".", "\\.")}\\s*\\{[^}]*background:\\s*var\\(--color-`, "s"));
    }
    expect(interaction).toMatch(/\.question input\s*\{[^}]*background:\s*var\(--color-/s);
    expect(style).toMatch(/@media\s*\(max-width:\s*800px\)/);
    expect(style).toMatch(/prefers-reduced-motion/);
  });
});
