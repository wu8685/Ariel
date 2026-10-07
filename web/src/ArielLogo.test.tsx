// @vitest-environment jsdom

import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ArielLogo, arielLogoAssets, resolveArielLogoVariant } from "./ArielLogo";

describe("ArielLogo", () => {
  it("uses the wind-messenger tier at every critical numeric size", () => {
    for (const size of [16, 24, 32, 64]) expect(resolveArielLogoVariant(size, "auto"), `${size}px`).toBe("micro");
    for (const size of [128, 256]) expect(resolveArielLogoVariant(size, "auto"), `${size}px`).toBe("mono");
    for (const size of [320, 512]) expect(resolveArielLogoVariant(size, "auto"), `${size}px`).toBe("color");
  });

  it("rejects auto selection for CSS string sizes and sub-minimum numbers", () => {
    expect(() => resolveArielLogoVariant("320px", "auto")).toThrow(/explicit variant/i);
    expect(() => resolveArielLogoVariant("clamp(128px, 12vw, 160px)", "auto")).toThrow(/explicit variant/i);
    expect(() => resolveArielLogoVariant(15, "auto")).toThrow(/at least 16/i);
    expect(resolveArielLogoVariant("10rem", "mono")).toBe("mono");
  });

  it("selects exactly one image element and the correct theme candidates", () => {
    const { rerender } = render(<ArielLogo size={64} alt="Ariel 微标" />);
    let logo = screen.getByRole("img", { name: "Ariel 微标" });
    expect(logo.getAttribute("data-variant")).toBe("micro");
    expect(logo.querySelectorAll("img")).toHaveLength(1);
    expect(logo.querySelector("img")?.getAttribute("src")).toBe(arielLogoAssets.microInk);
    expect(logo.querySelector("source")?.getAttribute("srcset")).toBe(arielLogoAssets.microWhite);

    rerender(<ArielLogo size={160} tone="auto" alt="Ariel 单色标" />);
    logo = screen.getByRole("img", { name: "Ariel 单色标" });
    expect(logo.getAttribute("data-variant")).toBe("mono");
    expect(logo.querySelectorAll("img")).toHaveLength(1);
    expect(logo.querySelector("img")?.getAttribute("src")).toBe(arielLogoAssets.ink);
    expect(logo.querySelector("source")?.getAttribute("srcset")).toBe(arielLogoAssets.white);

    rerender(<ArielLogo size={320} alt="Ariel 彩色标" />);
    logo = screen.getByRole("img", { name: "Ariel 彩色标" });
    expect(logo.getAttribute("data-variant")).toBe("color");
    expect(logo.querySelectorAll("img")).toHaveLength(1);
    expect(logo.querySelector("source")).toBeNull();
    expect(logo.querySelector("img")?.getAttribute("src")).toBe(arielLogoAssets.color);
  });

  it("honors explicit string-size variant, tone, priority and custom class", () => {
    render(<ArielLogo size="10rem" variant="mono" tone="white" priority alt="Ariel 反白标" className="custom-logo" />);
    const logo = screen.getByRole("img", { name: "Ariel 反白标" });
    const image = logo.querySelector("img")!;
    expect(logo.getAttribute("data-variant")).toBe("mono");
    expect(logo.getAttribute("data-tone")).toBe("white");
    expect(logo.classList.contains("custom-logo")).toBe(true);
    expect(image.getAttribute("src")).toBe(arielLogoAssets.white);
    expect(image.getAttribute("fetchpriority")).toBe("high");
    expect(image.getAttribute("loading")).toBe("eager");
  });

  it("selects the provided white micro asset without CSS recoloring", () => {
    const { container } = render(<ArielLogo size={29} variant="micro" tone="white" decorative />);
    expect(within(container).queryByRole("img")).toBeNull();
    const logo = container.querySelector(".ariel-logo--micro")!;
    expect(logo.getAttribute("aria-hidden")).toBe("true");
    expect(logo.querySelector("img")?.getAttribute("src")).toBe(arielLogoAssets.microWhite);
    expect((logo as HTMLElement).style.width).toBe("29px");
    expect((logo as HTMLElement).style.height).toBe("29px");
  });

  it("defaults an independent logo to the accessible name Ariel", () => {
    render(<ArielLogo size={128} variant="mono" tone="ink" />);
    expect(screen.getByRole("img", { name: "Ariel" })).toBeTruthy();
  });
});
