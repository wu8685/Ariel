// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { deriveAvatar, expressionForState, PuckAvatar } from "./PuckAvatar";

afterEach(cleanup);

describe("PuckAvatar", () => {
  it("derives a stable shape and color from the assistant id", () => {
    expect(deriveAvatar("assistant-a")).toEqual(deriveAvatar("assistant-a"));
    expect(deriveAvatar("assistant-a")).not.toEqual(deriveAvatar("assistant-b"));
    expect(["blob", "cloud", "star", "triangle"]).toContain(deriveAvatar("assistant-a").shape);
    expect(deriveAvatar("assistant-a").color).toMatch(/^#[0-9a-f]{6}$/);
  });

  it("maps real states to distinct expressions", () => {
    expect(expressionForState("working")).toBe("searching");
    expect(expressionForState("waiting_user")).toBe("waiting");
    expect(expressionForState("awaiting_authorization")).toBe("waiting");
    expect(expressionForState("error")).toBe("worried");
    expect(expressionForState("done")).toBe("happy");
  });

  it("is accessible, non-draggable and reacts to touch", () => {
    render(<PuckAvatar assistantId="assistant-a" name="小帕" state="waiting_user" size="large" attention />);
    const avatar = screen.getByRole("img", { name: "小帕，等待你的回复" });
    expect(avatar.getAttribute("draggable")).toBe("false");
    expect(avatar.getAttribute("data-expression")).toBe("waiting");
    expect(avatar.querySelector(".puck-avatar-badge")).toBeTruthy();
    fireEvent.pointerDown(avatar);
    expect(avatar.getAttribute("data-reacting")).toBe("true");
  });
});
