// @vitest-environment jsdom

import { createHash } from "node:crypto";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(process.cwd(), "..");
const brand = resolve(process.cwd(), "public/brand");
const assets = {
  "ariel-logo-wind-messenger-color.png": "691d4beab26ab10684353ac81f2ab61827c1c72b0c264668a7f82cd591306d30",
  "ariel-logo-wind-messenger-color-512.png": "faa20f8e37fa45905d0a9ec8a923b2edbebcf8a2ae507802470f8ca26c303e6d",
  "ariel-logo-wind-messenger-ink.png": "308a248ea542b8d7bd0a76e4aa2d82ffd5307b0263a7f00cd73b28238460df91",
  "ariel-logo-wind-messenger-white.png": "a85ec3b832dc15879dbff4a67ea444da7ee87e834ffcbcf0cb1a8eabd382c211",
  "ariel-logo-wind-messenger-micro.svg": "c87edfb3e3df51c91c94e50eaf5554ac9d104ba8dbbfe1e01792af776bdbd154",
  "ariel-logo-wind-messenger-micro-white.svg": "5de64a4a0b6b871e5a0d2839c8d1b4efed7a18eb2c7dee29c4f6618209b14f99",
} as const;

function walkText(directory: string): string {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) return walkText(path);
    if (entry.name === "brand-assets.test.ts") return [];
    return /\.(?:md|html|ts|tsx|css|json|webmanifest)$/.test(entry.name) ? [readFileSync(path, "utf8")] : [];
  }).join("\n");
}

describe("formal Ariel wind-messenger brand assets", () => {
  it("keeps byte-identical repository copies of the six approved runtime assets", () => {
    expect(readdirSync(brand).sort()).toEqual(Object.keys(assets).sort());
    for (const [name, expected] of Object.entries(assets)) {
      const bytes = readFileSync(resolve(brand, name));
      expect(createHash("sha256").update(bytes).digest("hex"), name).toBe(expected);
    }
  });

  it("preserves square RGBA PNG dimensions for transparent rendering", () => {
    const dimensions = {
      "ariel-logo-wind-messenger-color.png": 1254,
      "ariel-logo-wind-messenger-color-512.png": 512,
      "ariel-logo-wind-messenger-ink.png": 1254,
      "ariel-logo-wind-messenger-white.png": 1254,
    } as const;
    for (const [name, dimension] of Object.entries(dimensions)) {
      const png = readFileSync(resolve(brand, name));
      expect(png.subarray(1, 4).toString(), name).toBe("PNG");
      expect(png.readUInt32BE(16), name).toBe(dimension);
      expect(png.readUInt32BE(20), name).toBe(dimension);
      expect(png[24], name).toBe(8);
      expect(png[25], name).toBe(6);
    }
  });

  it("ships parseable ink and white micro marks with one core and six receivers", () => {
    for (const name of ["ariel-logo-wind-messenger-micro.svg", "ariel-logo-wind-messenger-micro-white.svg"]) {
      const svg = readFileSync(resolve(brand, name), "utf8");
      const parsed = new DOMParser().parseFromString(svg, "image/svg+xml");
      expect(parsed.querySelector("parsererror"), name).toBeNull();
      expect(parsed.documentElement.getAttribute("viewBox"), name).toBe("0 0 64 64");
      expect(parsed.querySelectorAll("circle"), name).toHaveLength(6);
      expect(parsed.querySelectorAll("filter, mask"), name).toHaveLength(0);
    }
  });

  it("uses repository-relative wind-messenger references in README, docs and web metadata", () => {
    const readme = readFileSync(resolve(root, "README.md"), "utf8");
    expect(readme).toMatch(/^<p align="center">\n  <img src="web\/public\/brand\/ariel-logo-wind-messenger-color\.png" width="480" alt="Ariel 风之信使彩色主版 Logo" \/>\n<\/p>\n\n# Ariel\n/);
    expect(readme.match(/ariel-logo-wind-messenger-color\.png/g)).toHaveLength(1);
    expect(readme).toContain("docs/brand/logo-guideline.html");
    expect(readme).toContain("docs/brand/brand-spec.md");

    const guideline = readFileSync(resolve(root, "docs/brand/logo-guideline.html"), "utf8");
    const spec = readFileSync(resolve(root, "docs/brand/brand-spec.md"), "utf8");
    expect(`${guideline}\n${spec}`).not.toContain("Application Support/Open Design");
    for (const name of Object.keys(assets)) expect(`${guideline}\n${spec}`, name).toContain(name);
    const references = [...guideline.matchAll(/(?:src|href)="([^"]+\.(?:png|svg))"/g)].map(match => match[1]);
    expect(references.length).toBeGreaterThan(10);
    for (const reference of references) expect(existsSync(resolve(root, "docs/brand", reference)), reference).toBe(true);

    const html = readFileSync(resolve(root, "web/index.html"), "utf8");
    const manifest = readFileSync(resolve(root, "web/public/manifest.webmanifest"), "utf8");
    expect(html).toContain("/brand/ariel-logo-wind-messenger-micro.svg");
    expect(html).toContain("/brand/ariel-logo-wind-messenger-color-512.png");
    expect(html).toContain('rel="apple-touch-icon"');
    expect(html).toContain('rel="manifest" href="/manifest.webmanifest"');
    expect(html).toContain('property="og:image"');
    expect(html).toContain('name="twitter:image"');
    expect(manifest).toContain("/brand/ariel-logo-wind-messenger-color-512.png");
  });

  it("contains no retired asset names in product, docs, tests or examples", () => {
    const text = [
      readFileSync(resolve(root, "README.md"), "utf8"),
      walkText(resolve(root, "docs")),
      walkText(resolve(root, "web/src")),
      walkText(resolve(root, "web/e2e")),
      readFileSync(resolve(root, "web/index.html"), "utf8"),
    ].join("\n");
    const retiredNames = new RegExp([
      ["storm", "harpy"].join("-"),
      ["ariel-logo", "mythic"].join("-"),
      ["ariel-logo", "micro\\.svg"].join("-"),
    ].join("|"));
    expect(text).not.toMatch(retiredNames);
  });
});
