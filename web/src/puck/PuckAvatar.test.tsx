// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import { avatarColors, avatarShapes, canonicalShape, deriveAvatar, expressionForState, nearestAvatarColor, PuckAvatar } from "./PuckAvatar";
import { resetAvatarMotionSchedulerForTests } from "./PuckAvatarScheduler";
import avatarSource from "./PuckAvatar.tsx?raw";
const avatarStyle = readFileSync("src/puck/puck.css", "utf8");

afterEach(() => { cleanup(); resetAvatarMotionSchedulerForTests(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("PuckAvatar", () => {
  it("derives a stable shape and color from the assistant id", () => {
    expect(deriveAvatar("assistant-a")).toEqual(deriveAvatar("assistant-a"));
    expect(deriveAvatar("assistant-a")).not.toEqual(deriveAvatar("assistant-b"));
    expect(avatarShapes.map(option => option.value)).toEqual([
      "blob", "cloud", "square", "sparkle", "clover", "heart", "flower", "teardrop", "capsule", "wedge", "house", "star", "hex",
    ]);
    expect(avatarColors.map(option => option.value)).toEqual(["white", "brown", "red", "orange", "yellow", "green", "cyan", "blue", "violet", "magenta", "gray"]);
    expect(avatarColors[0].hex).toBe("#ffffff");
    expect(avatarColors.find(option => option.value === "blue")?.hex).toBe("#3c82f6");
    expect(canonicalShape("pebble")).toBe("pebble");
    expect(nearestAvatarColor("black")).toBe("black");
    expect(nearestAvatarColor("#ffffff")).toBe("white");
    expect(avatarShapes.map(option => option.value)).toContain(deriveAvatar("assistant-a").shape);
    expect(avatarColors.map(option => option.hex)).toContain(deriveAvatar("assistant-a").colorHex);
    expect(canonicalShape("pumpkin")).toBe("dome");
    expect(nearestAvatarColor("#ff2f92")).toBe("magenta");
  });

  it("maps real states to distinct expressions", () => {
    expect(expressionForState("working")).toBe("searching");
    expect(expressionForState("waiting_user")).toBe("waiting");
    expect(expressionForState("awaiting_authorization")).toBe("waiting");
    expect(expressionForState("error")).toBe("worried");
    expect(expressionForState("done")).toBe("happy");
  });

	it("does not draw a presence ring and renders every picker shape once", () => {
		const { container } = render(<>{avatarShapes.map(option => <PuckAvatar key={option.value} assistantId={option.value} name={option.label} design={{ shape: option.value, color: "cyan", colorHex: "#00bca6" }} animated={false}/>)}</>);
		const avatars = Array.from(container.querySelectorAll<HTMLElement>(".puck-avatar"));
		expect(avatars).toHaveLength(13);
		expect(container.querySelector(".puck-avatar-presence")).toBeNull();
		expect(avatarStyle).not.toMatch(/puck-avatar-presence/);
		expect(avatarStyle).toMatch(/--puck-avatar-row:\s*44px/);
		expect(avatarStyle).toMatch(/--puck-avatar-gap:\s*17px/);
		expect(avatarStyle).toMatch(/--puck-row-height:\s*96px/);
		expect(avatarStyle).toMatch(/--puck-sidebar-width:\s*280px/);
		expect(avatarStyle).toMatch(/\.puck-assistant-row\s*\{[^}]*min-height:\s*var\(--puck-row-height\)/s);
		for (const avatar of avatars) {
			expect(avatar.querySelectorAll(".puck-avatar-ear")).toHaveLength(0);
			expect(avatar.querySelector(".puck-avatar-body path")?.getAttribute("d")).toBeTruthy();
		}
	});

	it("renders a stored photo instead of the shape and keeps the shape available without one", () => {
		const photo = "data:image/png;base64,AAAA";
		const { container } = render(<PuckAvatar assistantId="assistant-photo" name="照片" design={{ shape: "blob", color: "black", colorHex: "#000000", image: photo }} animated={false}/>);
		expect(container.querySelector("img")?.getAttribute("src")).toBe(photo);
		expect(container.querySelector(".puck-avatar-body")).toBeNull();
	});

	it("renders two slanted dark capsule eyes directly on one moving face", () => {
		const { container } = render(<PuckAvatar assistantId="assistant-eyes" name="眼睛测试" animated={false}/>);
		const face = container.querySelector(".puck-avatar-face");
		const eyes = Array.from(face?.querySelectorAll(".puck-avatar-eye-shape") || []);
		expect(eyes).toHaveLength(2);
		expect(eyes.every(eye => eye.tagName.toLowerCase() === "rect")).toBe(true);
		expect(face?.querySelector(".puck-avatar-eyes")?.getAttribute("transform")).toContain("rotate(-18");
		expect(eyes.map(eye => [eye.getAttribute("width"), eye.getAttribute("height"), eye.getAttribute("rx")])).toEqual([
			["7", "14", "3.5"],
			["7", "14", "3.5"],
		]);
		expect(Number(eyes[0].getAttribute("y"))).toBeLessThan(47);
		expect(Number(eyes[1].getAttribute("x")) - Number(eyes[0].getAttribute("x"))).toBe(10);
		expect(face?.querySelector("ellipse")).toBeNull();
		expect(container.querySelector(".puck-avatar-eye-white, .puck-avatar-pupil, clipPath")).toBeNull();
		expect(container.querySelector(".puck-avatar-body")?.getAttribute("style")).toBeNull();
	});

	it("keeps the body and face in one moving head group", () => {
		const { container } = render(<PuckAvatar assistantId="assistant-head" name="头部测试" animated={false}/>);
		const head = container.querySelector(".puck-avatar-head");
		expect(head).not.toBeNull();
		expect(head?.querySelector(":scope > .puck-avatar-body")).not.toBeNull();
		expect(head?.querySelector(":scope > .puck-avatar-face")).not.toBeNull();
	});

  it("is accessible, non-draggable and reacts to touch", () => {
    render(<PuckAvatar assistantId="assistant-a" name="小帕" state="waiting_user" size="large" attention />);
    const avatar = screen.getByRole("img", { name: "小帕，等待你的回复" });
    expect(avatar.getAttribute("draggable")).toBe("false");
    expect(avatar.getAttribute("data-expression")).toBe("waiting");
		expect(avatar.querySelector(".puck-avatar-badge")).toBeTruthy();
		expect(avatar.querySelectorAll(".puck-avatar-ear")).toHaveLength(0);
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

	it("makes the eye nearest the gaze direction smaller during pointer turns", () => {
		const { container } = render(<PuckAvatar assistantId="assistant-perspective" name="透视测试" animated={false}/>);
		const avatar = container.querySelector<HTMLElement>(".puck-avatar")!;
		vi.spyOn(avatar, "getBoundingClientRect").mockReturnValue({ x: 0, y: 0, left: 0, top: 0, right: 100, bottom: 100, width: 100, height: 100, toJSON: () => ({}) });

		fireEvent.pointerMove(avatar, { clientX: 0, clientY: 50, pointerType: "mouse" });
		expect(Number(avatar.style.getPropertyValue("--puck-left-eye-xscale"))).toBeLessThan(Number(avatar.style.getPropertyValue("--puck-right-eye-xscale")));
		fireEvent.pointerMove(avatar, { clientX: 100, clientY: 50, pointerType: "mouse" });
		expect(Number(avatar.style.getPropertyValue("--puck-right-eye-xscale"))).toBeLessThan(Number(avatar.style.getPropertyValue("--puck-left-eye-xscale")));
		fireEvent.pointerMove(avatar, { clientX: 50, clientY: 0, pointerType: "mouse" });
		expect(avatar.style.getPropertyValue("--puck-left-eye-xscale")).toBe(avatar.style.getPropertyValue("--puck-right-eye-xscale"));
		expect(Math.abs(Number.parseFloat(avatar.style.getPropertyValue("--puck-head-y")))).toBeGreaterThanOrEqual(6);
		expect(Math.abs(Number.parseFloat(avatar.style.getPropertyValue("--puck-face-y")))).toBeGreaterThanOrEqual(9);
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
		expect(avatarStyle).toMatch(/\[data-moving="true"\][^{]*\{[^}]*will-change:\s*transform/s);
		expect(avatarStyle).not.toMatch(/\.puck-avatar-head\s*\{[^}]*will-change/s);
		expect(avatarStyle).toMatch(/--puck-motion-easing:\s*cubic-bezier\(\.45,\s*0,\s*\.25,\s*1\)/);
		expect(avatarStyle).toMatch(/transition:\s*transform var\(--puck-motion-duration\) var\(--puck-motion-easing\)/);
		expect(avatarStyle).not.toMatch(/\.puck-avatar-eye-(?:left|right)[^{]*\{[^}]*rotate/s);
	});

	it("adds will-change only while a transform transition is active", () => {
		const { container } = render(<PuckAvatar assistantId="assistant-transition" name="过渡测试" animated={false}/>);
		const avatar = container.querySelector<HTMLElement>(".puck-avatar")!;
		const head = container.querySelector(".puck-avatar-head")!;
		const run = new Event("transitionrun", { bubbles: true });
		Object.defineProperty(run, "propertyName", { value: "transform" });
		head.dispatchEvent(run);
		expect(avatar.dataset.moving).toBe("true");
		const end = new Event("transitionend", { bubbles: true });
		Object.defineProperty(end, "propertyName", { value: "transform" });
		head.dispatchEvent(end);
		expect(avatar.dataset.moving).toBe("false");
	});
});
