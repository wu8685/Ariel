// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { avatarColors, avatarShapes, deriveAvatar, expressionForState, PuckAvatar } from "./PuckAvatar";
import { resetAvatarMotionSchedulerForTests } from "./PuckAvatarScheduler";
import avatarSource from "./PuckAvatar.tsx?raw";
import avatarStyle from "./puck.css?raw";

afterEach(() => { cleanup(); resetAvatarMotionSchedulerForTests(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

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

	it("renders two parallel black capsule eyes directly on one moving face", () => {
		const { container } = render(<PuckAvatar assistantId="assistant-eyes" name="眼睛测试" animated={false}/>);
		const face = container.querySelector(".puck-avatar-face");
		const eyes = Array.from(face?.querySelectorAll(".puck-avatar-eye-shape") || []);
		expect(eyes).toHaveLength(2);
		expect(eyes.map(eye => [eye.getAttribute("rx"), eye.getAttribute("ry")])).toEqual([["4.6", "8"], ["4.6", "8"]]);
		expect(container.querySelector(".puck-avatar-eye-white, .puck-avatar-pupil, clipPath")).toBeNull();
		expect(container.querySelector(".puck-avatar-body")?.getAttribute("style")).toBeNull();
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

	it("does not re-render while the shared scheduler changes gaze and blink phases", async () => {
		vi.useFakeTimers();
		let renders = 0;
		function CountedAvatar() { renders += 1; return <PuckAvatar assistantId="assistant-counted" name="计数测试"/>; }
		render(<CountedAvatar/>);
		const initialRenders = renders;
		await act(() => vi.advanceTimersByTimeAsync(20_000));
		expect(renders).toBe(initialRenders);
		const avatar = screen.getByRole("img", { name: "计数测试，空闲" });
		expect(Number(avatar.getAttribute("data-motion-count"))).toBeGreaterThan(0);
	});

	it("contains no per-avatar timer loop, React motion state, infinite body animation or permanent will-change", () => {
		expect(avatarSource).not.toMatch(/\buseState\b|requestAnimationFrame|setInterval|setTimeout/);
		expect(avatarStyle).not.toMatch(/\.puck-workspace \.puck-avatar svg[^}]*animation:/s);
		expect(avatarStyle).not.toMatch(/\.puck-workspace \.puck-avatar-body[^}]*animation:/s);
		expect(avatarStyle).not.toMatch(/will-change/);
		expect(avatarStyle).not.toMatch(/\.puck-avatar-eye-(?:left|right)[^{]*\{[^}]*rotate/s);
	});
});
