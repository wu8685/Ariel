// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { configureIOSInputViewport } from "./viewport";

function viewportDocument(content = "width=device-width, initial-scale=1.0"): Document {
  const target = document.implementation.createHTMLDocument();
  const viewport = target.createElement("meta");
  viewport.name = "viewport";
  viewport.content = content;
  target.head.append(viewport);
  return target;
}

describe("iOS input viewport", () => {
  it("disables automatic focus zoom on iPhone without duplicating the scale token", () => {
    const target = viewportDocument("width=device-width, initial-scale=1.0, maximum-scale=2");
    const navigator = { userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X)", platform: "iPhone", maxTouchPoints: 5 };
    expect(configureIOSInputViewport(target, navigator)).toBe(true);
    expect(configureIOSInputViewport(target, navigator)).toBe(true);
    const content = target.querySelector<HTMLMetaElement>('meta[name="viewport"]')?.content || "";
    expect(content).toContain("maximum-scale=1");
    expect(content.match(/maximum-scale/g)).toHaveLength(1);
  });

  it("recognizes iPad desktop mode", () => {
    const target = viewportDocument();
    expect(configureIOSInputViewport(target, { userAgent: "Mozilla/5.0 (Macintosh)", platform: "MacIntel", maxTouchPoints: 5 })).toBe(true);
    expect(target.querySelector<HTMLMetaElement>('meta[name="viewport"]')?.content).toContain("maximum-scale=1");
  });

  it("does not change Android or desktop viewports", () => {
    for (const navigator of [
      { userAgent: "Mozilla/5.0 (Linux; Android 16)", platform: "Linux armv8l", maxTouchPoints: 5 },
      { userAgent: "Mozilla/5.0 (Macintosh)", platform: "MacIntel", maxTouchPoints: 0 },
    ]) {
      const target = viewportDocument();
      expect(configureIOSInputViewport(target, navigator)).toBe(false);
      expect(target.querySelector<HTMLMetaElement>('meta[name="viewport"]')?.content).toBe("width=device-width, initial-scale=1.0");
    }
  });
});
