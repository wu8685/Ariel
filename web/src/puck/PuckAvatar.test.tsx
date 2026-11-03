// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { deriveAvatar, expressionForState, PuckAvatar } from "./PuckAvatar";

afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

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
		expect(avatar.querySelectorAll(".puck-avatar-ear")).toHaveLength(2);
		expect(avatar.querySelectorAll(".puck-avatar-leaf")).toHaveLength(2);
		expect(avatar.querySelector(".puck-avatar-brows")).toBeNull();
    fireEvent.pointerDown(avatar);
    expect(avatar.getAttribute("data-reacting")).toBe("true");
		expect(avatar.querySelectorAll(".puck-avatar-particle")).toHaveLength(5);
  });

	it("never renders eyebrows for any state", () => {
		const states = ["idle", "working", "waiting_user", "awaiting_authorization", "attention", "error", "done"];
		const { rerender, container } = render(<PuckAvatar assistantId="assistant-a" name="小帕" state={states[0]} />);
		for (const state of states) {
			rerender(<PuckAvatar assistantId="assistant-a" name="小帕" state={state} />);
			expect(container.querySelector(".puck-avatar-brows")).toBeNull();
		}
	});

	it("clears its playful particles and suppresses motion when reduced motion is requested", async () => {
		vi.useFakeTimers();
		const normal = render(<PuckAvatar assistantId="assistant-a" name="小帕" />);
		const avatar = normal.getByRole("img", { name: "小帕，空闲" });
		fireEvent.pointerDown(avatar);
		expect(avatar.querySelector(".puck-avatar-particles")).toBeTruthy();
		await act(() => vi.advanceTimersByTimeAsync(420));
		expect(avatar.querySelector(".puck-avatar-particles")).toBeNull();
		normal.unmount();

		vi.stubGlobal("matchMedia", vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
		render(<PuckAvatar assistantId="assistant-b" name="小精灵" />);
		const reduced = screen.getByRole("img", { name: "小精灵，空闲" });
		fireEvent.pointerDown(reduced);
		expect(reduced.getAttribute("data-reacting")).toBe("false");
		expect(reduced.querySelector(".puck-avatar-particles")).toBeNull();
	});

  it("schedules a non-periodic blink while visible", async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    render(<PuckAvatar assistantId="assistant-a" name="小帕" />);
    const avatar = screen.getByRole("img", { name: "小帕，空闲" });
    expect(avatar.getAttribute("data-blinking")).toBe("false");
    await act(() => vi.advanceTimersByTimeAsync(2200));
    expect(avatar.getAttribute("data-blinking")).toBe("true");
    await act(() => vi.advanceTimersByTimeAsync(140));
    expect(avatar.getAttribute("data-blinking")).toBe("false");
  });
});
