// @vitest-environment jsdom

import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(process.cwd(), "..");
const brand = resolve(process.cwd(), "public/brand");
const assets = {
  "ariel-logo-mythic-color.png": "1d0b1387b665853ae23f920a1d3cf53488e32189e9a86e4a232844fd70f88d33",
  "ariel-logo-mythic-ink.png": "f79df791d9e0e7b18f638747afcfdf4106bb12f2dbfdd5331d60c6306111079d",
  "ariel-logo-mythic-white.png": "93e800f7c16d982aa4c7a9d8926354fa6b681e073a30682aa60ba1a9e5671ab9",
  "ariel-logo-micro.svg": "658c049b3a31140caf0388cf8b3a4d1ee1879956344190814b05a202237eebdc",
} as const;

describe("formal Ariel brand assets", () => {
  it("keeps byte-identical repository copies of all approved source assets", () => {
    for (const [name, expected] of Object.entries(assets)) {
      const bytes = readFileSync(resolve(brand, name));
      expect(createHash("sha256").update(bytes).digest("hex"), name).toBe(expected);
    }
  });

  it("preserves PNG dimensions and alpha contracts", () => {
    for (const [name, colorType] of [["ariel-logo-mythic-color.png", 2], ["ariel-logo-mythic-ink.png", 6], ["ariel-logo-mythic-white.png", 6]] as const) {
      const png = readFileSync(resolve(brand, name));
      expect(png.subarray(1, 4).toString(), name).toBe("PNG");
      expect(png.readUInt32BE(16), name).toBe(1254);
      expect(png.readUInt32BE(20), name).toBe(1254);
      expect(png[25], name).toBe(colorType);
    }
  });

  it("ships a parseable 64px micro mark and complete repository-relative documentation", () => {
    const svg = readFileSync(resolve(brand, "ariel-logo-micro.svg"), "utf8");
    const parsed = new DOMParser().parseFromString(svg, "image/svg+xml");
    expect(parsed.querySelector("parsererror")).toBeNull();
    expect(parsed.documentElement.getAttribute("viewBox")).toBe("0 0 64 64");

    const readme = readFileSync(resolve(root, "README.md"), "utf8");
    expect(readme).toMatch(/web\/public\/brand\/ariel-logo-mythic-color\.png[^>]*width="480"/);
    expect(readme).toContain("docs/brand/logo-guideline.html");
    expect(readme).toContain("docs/brand/brand-spec.md");
    for (const path of ["docs/brand/logo-guideline.html", "docs/brand/brand-spec.md"]) expect(existsSync(resolve(root, path)), path).toBe(true);

    const guideline = readFileSync(resolve(root, "docs/brand/logo-guideline.html"), "utf8");
    const spec = readFileSync(resolve(root, "docs/brand/brand-spec.md"), "utf8");
    expect(`${guideline}\n${spec}`).not.toContain("Application Support/Open Design");
    for (const name of Object.keys(assets)) expect(`${guideline}\n${spec}`, name).toContain(name);
    const references = [...guideline.matchAll(/(?:src|href)="([^"]+\.(?:png|svg))"/g)].map(match => match[1]);
    expect(references.length).toBeGreaterThan(10);
    for (const reference of references) expect(existsSync(resolve(root, "docs/brand", reference)), reference).toBe(true);
  });
});
