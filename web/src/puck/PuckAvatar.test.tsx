// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { avatarColors, avatarShapes, deriveAvatar, expressionForState, PuckAvatar } from "./PuckAvatar";

afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("PuckAvatar", () => {
  it("derives a stable shape and color from the assistant id", () => {
    expect(deriveAvatar("assistant-a")).toEqual(deriveAvatar("assistant-a"));
    expect(deriveAvatar("assistant-a")).not.toEqual(deriveAvatar("assistant-b"));
    expect(avatarShapes.map(option => option.value)).toEqual([
      "circle", "cloud", "tilted_square", "four_star", "clover", "heart", "flower",
      "drop", "capsule", "triangle", "tilted_rect", "burst", "hexagon", "pumpkin",
    ]);
    expect(avatarColors.map(option => option.label)).toEqual(["白", "棕", "红", "橙", "琥珀", "绿", "青绿", "蓝", "紫", "粉", "灰"]);
    expect(avatarShapes.map(option => option.value)).toContain(deriveAvatar("assistant-a").shape);
    expect(deriveAvatar("assistant-a").color).toMatch(/^#[0-9a-f]{6}$/);
  });

  it("maps real states to distinct expressions", () => {
    expect(expressionForState("working")).toBe("searching");
    expect(expressionForState("waiting_user")).toBe("waiting");
    expect(expressionForState("awaiting_authorization")).toBe("waiting");
    expect(expressionForState("error")).toBe("worried");
    expect(expressionForState("done")).toBe("happy");
  });

	it("renders every shape with exactly two ears and no botanical or facial extras", () => {
		const { container } = render(<>{avatarShapes.map(option => <PuckAvatar key={option.value} assistantId={option.value} name={option.label} design={{ shape: option.value, color: "#1db9aa" }} animated={false}/>)}</>);
		const avatars = Array.from(container.querySelectorAll<HTMLElement>(".puck-avatar"));
		expect(avatars).toHaveLength(14);
		for (const avatar of avatars) {
			expect(avatar.querySelectorAll(".puck-avatar-ear")).toHaveLength(2);
			expect(avatar.querySelector(".puck-avatar-body")?.childElementCount).toBeGreaterThan(0);
			expect(avatar.querySelector(".puck-avatar-crown, .puck-avatar-leaf, .puck-avatar-flower, .puck-avatar-mouth, .puck-avatar-brows")).toBeNull();
		}
	});

  it("is accessible, non-draggable and reacts to touch", () => {
    render(<PuckAvatar assistantId="assistant-a" name="小帕" state="waiting_user" size="large" attention />);
    const avatar = screen.getByRole("img", { name: "小帕，等待你的回复" });
    expect(avatar.getAttribute("draggable")).toBe("false");
    expect(avatar.getAttribute("data-expression")).toBe("waiting");
    expect(avatar.querySelector(".puck-avatar-badge")).toBeTruthy();
		expect(avatar.querySelectorAll(".puck-avatar-ear")).toHaveLength(2);
		expect(avatar.querySelector(".puck-avatar-crown")).toBeNull();
		expect(avatar.querySelector(".puck-avatar-leaf")).toBeNull();
		expect(avatar.querySelector(".puck-avatar-flower")).toBeNull();
		expect(avatar.querySelector(".puck-avatar-mouth")).toBeNull();
		expect(avatar.querySelector(".puck-avatar-brows")).toBeNull();
    fireEvent.pointerDown(avatar);
    expect(avatar.getAttribute("data-reacting")).toBe("true");
		expect(avatar.querySelectorAll(".puck-avatar-sparkle")).toHaveLength(5);
		expect(avatar.querySelector(".puck-avatar-particle")).toBeNull();
  });

	it("never renders eyebrows for any state", () => {
		const states = ["idle", "working", "waiting_user", "awaiting_authorization", "attention", "error", "done"];
		const { rerender, container } = render(<PuckAvatar assistantId="assistant-a" name="小帕" state={states[0]} />);
		for (const state of states) {
			rerender(<PuckAvatar assistantId="assistant-a" name="小帕" state={state} />);
			expect(container.querySelector(".puck-avatar-brows")).toBeNull();
		}
	});

	it("clears its playful sparkles and suppresses non-blink motion when reduced motion is requested", async () => {
		vi.useFakeTimers();
		const normal = render(<PuckAvatar assistantId="assistant-a" name="小帕" />);
		const avatar = normal.getByRole("img", { name: "小帕，空闲" });
		fireEvent.pointerDown(avatar);
		expect(avatar.querySelector(".puck-avatar-sparkles")).toBeTruthy();
		await act(() => vi.advanceTimersByTimeAsync(420));
		expect(avatar.querySelector(".puck-avatar-sparkles")).toBeNull();
		normal.unmount();

		vi.stubGlobal("matchMedia", vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
		render(<PuckAvatar assistantId="assistant-b" name="小精灵" />);
		const reduced = screen.getByRole("img", { name: "小精灵，空闲" });
		fireEvent.pointerDown(reduced);
		expect(reduced.getAttribute("data-reacting")).toBe("false");
		expect(reduced.querySelector(".puck-avatar-sparkles")).toBeNull();
	});

  it("schedules random blink, double-blink, head tilt and eye look with staggered phases", async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    const view = render(<><PuckAvatar assistantId="assistant-a" name="小帕" /><PuckAvatar assistantId="assistant-a" name="小帕二号" /></>);
    const avatar = screen.getByRole("img", { name: "小帕，空闲" });
    const peer = screen.getByRole("img", { name: "小帕二号，空闲" });
    expect(avatar.getAttribute("data-motion-phase")).not.toBe(peer.getAttribute("data-motion-phase"));
    const blinkDelay = Number(avatar.getAttribute("data-blink-delay"));
    expect(avatar.getAttribute("data-blinking")).toBe("false");
    await act(() => vi.advanceTimersByTimeAsync(blinkDelay));
    expect(avatar.getAttribute("data-blinking")).toBe("true");
    await act(() => vi.advanceTimersByTimeAsync(230));
    expect(Number(avatar.getAttribute("data-blink-count"))).toBeGreaterThanOrEqual(2);
    view.unmount();

    const tiltView = render(<PuckAvatar assistantId="assistant-tilt" name="歪头测试" />);
    const tilting = screen.getByRole("img", { name: "歪头测试，空闲" });
    await act(() => vi.advanceTimersByTimeAsync(Number(tilting.getAttribute("data-tilt-delay"))));
    expect(tilting.getAttribute("data-tilting")).toBe("true");
    tiltView.unmount();

    render(<PuckAvatar assistantId="assistant-look" name="视线测试" />);
    const looking = screen.getByRole("img", { name: "视线测试，空闲" });
    await act(() => vi.advanceTimersByTimeAsync(Number(looking.getAttribute("data-look-delay"))));
    expect(looking.getAttribute("data-looking")).not.toBe("0");
  });

	it("keeps only blinking autonomous motion in reduced-motion mode", async () => {
		vi.useFakeTimers();
		vi.stubGlobal("matchMedia", vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
		render(<PuckAvatar assistantId="assistant-reduced" name="低动态" />);
		const avatar = screen.getByRole("img", { name: "低动态，空闲" });
		await act(() => vi.advanceTimersByTimeAsync(20_000));
		expect(Number(avatar.getAttribute("data-blink-count"))).toBeGreaterThan(0);
		expect(avatar.getAttribute("data-tilting")).toBe("false");
		expect(avatar.getAttribute("data-looking")).toBe("0");
	});

	it("pauses autonomous timers while the tab is hidden", async () => {
		vi.useFakeTimers();
		let hidden = false;
		vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
		render(<PuckAvatar assistantId="assistant-hidden" name="隐藏测试" />);
		const avatar = screen.getByRole("img", { name: "隐藏测试，空闲" });
		hidden = true;
		fireEvent(document, new Event("visibilitychange"));
		expect(avatar.getAttribute("data-paused")).toBe("true");
		const blinkCount = avatar.getAttribute("data-blink-count");
		await act(() => vi.advanceTimersByTimeAsync(20_000));
		expect(avatar.getAttribute("data-blink-count")).toBe(blinkCount);
		expect(avatar.getAttribute("data-tilting")).toBe("false");
		expect(avatar.getAttribute("data-looking")).toBe("0");
	});
});
