// @vitest-environment jsdom

import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ArielLogo, resolveArielLogoVariant } from "./ArielLogo";

describe("ArielLogo", () => {
  it("keeps the formal 128px and 320px tier boundaries", () => {
    expect(resolveArielLogoVariant(127, "auto")).toBe("micro");
    expect(resolveArielLogoVariant(128, "auto")).toBe("mono");
    expect(resolveArielLogoVariant("319px", "auto")).toBe("mono");
    expect(resolveArielLogoVariant("320px", "auto")).toBe("mythic");
    expect(resolveArielLogoVariant("10rem", "auto")).toBe("micro");
  });

  it("selects the formal asset tier from a numeric display size", () => {
    const { rerender } = render(<ArielLogo size={64} alt="Ariel 微标" />);
    expect(screen.getByRole("img", { name: "Ariel 微标" }).getAttribute("data-variant")).toBe("micro");

    rerender(<ArielLogo size={160} alt="Ariel 单色标" />);
    const mono = screen.getByRole("img", { name: "Ariel 单色标" });
    expect(mono.getAttribute("data-variant")).toBe("mono");
    expect(mono.querySelector("img")?.getAttribute("src")).toBe("/brand/ariel-logo-mythic-ink.png");
    expect(mono.querySelector("source")?.getAttribute("srcset")).toBe("/brand/ariel-logo-mythic-white.png");

    rerender(<ArielLogo size={360} alt="Ariel 彩色神话标" />);
    const mythic = screen.getByRole("img", { name: "Ariel 彩色神话标" });
    expect(mythic.getAttribute("data-variant")).toBe("mythic");
    expect(mythic.querySelector("img")?.getAttribute("src")).toBe("/brand/ariel-logo-mythic-color.png");
  });

  it("honors explicit variant and tone without filtering or distorting the asset", () => {
    const { container } = render(<ArielLogo size="10rem" variant="mono" tone="white" alt="Ariel 反白标" className="custom-logo" />);
    const logo = screen.getByRole("img", { name: "Ariel 反白标" });
    expect(logo.getAttribute("data-variant")).toBe("mono");
    expect(logo.getAttribute("data-tone")).toBe("white");
    expect(logo.classList.contains("custom-logo")).toBe(true);
    expect(logo.querySelector("img")?.getAttribute("src")).toBe("/brand/ariel-logo-mythic-white.png");
    expect(container.innerHTML).not.toContain("filter");
  });

  it("keeps decorative marks out of the accessibility tree", () => {
    const { container } = render(<ArielLogo size={29} variant="micro" decorative />);
    expect(within(container).queryByRole("img")).toBeNull();
    const logo = container.querySelector(".ariel-logo--micro");
    expect(logo?.getAttribute("aria-hidden")).toBe("true");
    expect((logo as HTMLElement).style.width).toBe("29px");
  });
});
