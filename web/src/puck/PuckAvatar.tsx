import { useEffect, useId, useMemo, useRef, type CSSProperties, type PointerEvent, type ReactNode } from "react";
import { registerAvatarMotion, restPoseForKey, type AvatarPose, type BlinkPhase } from "./PuckAvatarScheduler";

export type AvatarShape = "blob" | "pebble" | "bean" | "egg" | "squircle" | "tablet" | "capsule" | "cylinder" | "hex" | "gem" | "crystal" | "wedge" | "shield" | "dome" | "arch" | "cloud" | "teardrop" | "leaf" | "square" | "sparkle" | "clover" | "flower" | "heart" | "house" | "star";
export type AvatarColor = "black" | "white" | "brown" | "red" | "orange" | "yellow" | "green" | "cyan" | "blue" | "violet" | "magenta" | "gray";
export type PuckState = "idle" | "thinking" | "working" | "waiting_user" | "awaiting_authorization" | "attention" | "error" | "done" | string;
export type AvatarDesign = { shape: AvatarShape; color: AvatarColor; colorHex: string; image?: string };

const knownShapes: readonly AvatarShape[] = ["blob", "pebble", "bean", "egg", "squircle", "tablet", "capsule", "cylinder", "hex", "gem", "crystal", "wedge", "shield", "dome", "arch", "cloud", "teardrop", "leaf", "square", "sparkle", "clover", "flower", "heart", "house", "star"];

// Visible picker order matches the supplied avatar screenshot: 4 + 4 + 5.
// Stored ids outside this list still render, but are not offered as choices.
export const avatarShapes: ReadonlyArray<{ value: AvatarShape; label: string }> = [
  { value: "blob", label: "团块" }, { value: "cloud", label: "云" }, { value: "square", label: "方形" }, { value: "sparkle", label: "闪光" },
  { value: "clover", label: "四叶" }, { value: "heart", label: "心" }, { value: "flower", label: "花" }, { value: "teardrop", label: "水滴" },
  { value: "capsule", label: "胶囊" }, { value: "wedge", label: "楔形" }, { value: "house", label: "房子" }, { value: "star", label: "星" },
  { value: "hex", label: "六边形" },
];

// Swatch interiors sampled from the supplied dark-mode picker screenshot.
// The first swatch is white. Black remains a stored color and is not shown.
export const avatarColors: ReadonlyArray<{ value: AvatarColor; label: string; hex: string }> = [
  { value: "white", label: "白", hex: "#ffffff" }, { value: "brown", label: "棕", hex: "#8c6640" },
  { value: "red", label: "红", hex: "#ea4045" }, { value: "orange", label: "橙", hex: "#ed702d" },
  { value: "yellow", label: "黄", hex: "#f19d38" }, { value: "green", label: "绿", hex: "#5ac67a" },
  { value: "cyan", label: "青", hex: "#54b9a6" }, { value: "blue", label: "蓝", hex: "#3c82f6" },
  { value: "violet", label: "紫", hex: "#895bf5" }, { value: "magenta", label: "品红", hex: "#eb4698" },
  { value: "gray", label: "灰", hex: "#777777" },
];

const avatarPalette: ReadonlyArray<{ value: AvatarColor; hex: string }> = [
  { value: "black", hex: "#000000" },
  ...avatarColors,
];

const legacyShapes: Record<string, AvatarShape> = {
  circle: "blob", tilted_square: "square", four_star: "sparkle", drop: "teardrop",
  triangle: "wedge", tilted_rect: "tablet", burst: "star", hexagon: "hex", pumpkin: "dome",
};

function hash(value: string): number {
  let result = 2166136261;
  for (let index = 0; index < value.length; index += 1) result = Math.imul(result ^ value.charCodeAt(index), 16777619);
  return result >>> 0;
}

export function canonicalShape(shape: string): AvatarShape {
  if ((knownShapes as readonly string[]).includes(shape)) return shape as AvatarShape;
  return legacyShapes[shape] || "blob";
}

function channel(hex: string, index: number): number {
  return Number.parseInt(hex.slice(index, index + 2), 16);
}

export function nearestAvatarColor(color: string): AvatarColor {
  const named = avatarPalette.find(option => option.value === color);
  if (named) return named.value;
  const hex = color.trim().toLowerCase();
  if (!/^#[0-9a-f]{6}$/.test(hex)) return "black";
  let best: AvatarColor = "black";
  let score = Number.POSITIVE_INFINITY;
  for (const option of avatarPalette) {
    const distance = [1, 3, 5].reduce((sum, index) => sum + (channel(hex, index) - channel(option.hex, index)) ** 2, 0);
    if (distance < score) { score = distance; best = option.value; }
  }
  return best;
}

export function avatarColorHex(color: string): string {
  const named = avatarPalette.find(option => option.value === color);
  if (named) return named.hex;
  return /^#[0-9a-fA-F]{6}$/.test(color) ? color.toLowerCase() : "#000000";
}

export function avatarImage(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const image = value.trim();
  if (image.length > 262144) return undefined;
  if (/^data:image\/(?:png|jpeg|webp);base64,[a-z0-9+/=\s]+$/i.test(image)) return image;
  if (/^https:\/\/[^\s]+$/i.test(image)) return image;
  return undefined;
}

export function deriveAvatar(assistantId: string): AvatarDesign {
  const value = hash(assistantId || "puck");
  const color = avatarColors[(value >>> 8) % avatarColors.length];
  return { shape: avatarShapes[value % avatarShapes.length].value, color: color.value, colorHex: color.hex };
}

export function expressionForState(state: PuckState): "calm" | "searching" | "waiting" | "worried" | "happy" {
  if (state === "thinking" || state === "working") return "searching";
  if (state === "waiting_user" || state === "awaiting_authorization") return "waiting";
  if (state === "attention" || state === "error") return "worried";
  if (state === "done") return "happy";
  return "calm";
}

export function stateLabel(state: PuckState): string {
  if (state === "thinking" || state === "working") return "正在工作";
  if (state === "waiting_user") return "等待你的回复";
  if (state === "awaiting_authorization") return "等待你的授权";
  if (state === "attention" || state === "error") return "需要关注";
  if (state === "done") return "已完成";
  return "空闲";
}

function applyPose(element: HTMLElement, pose: AvatarPose) {
  element.dataset.pose = pose.name;
  element.style.setProperty("--puck-motion-duration", `${pose.motionDuration}ms`);
  element.style.setProperty("--puck-head-x", `${pose.headX}%`);
  element.style.setProperty("--puck-head-y", `${pose.headY}%`);
  element.style.setProperty("--puck-head-rotate", `${pose.headRotate}deg`);
  element.style.setProperty("--puck-face-x", `${pose.faceX}px`);
  element.style.setProperty("--puck-face-y", `${pose.faceY}px`);
  element.style.setProperty("--puck-face-rotate", `${pose.faceRotate}deg`);
  element.style.setProperty("--puck-left-eye-xscale", String(pose.leftEyeScaleX));
  element.style.setProperty("--puck-right-eye-xscale", String(pose.rightEyeScaleX));
  element.style.setProperty("--puck-left-eye-yscale", String(pose.leftEyeScaleY));
  element.style.setProperty("--puck-right-eye-yscale", String(pose.rightEyeScaleY));
  element.style.setProperty("--puck-eye-roundness", String(pose.eyeRoundness));
}

// Picker silhouettes are smoothed outer contours of the supplied screenshot.
// Simplification drops the finest scallops. Hidden stored shapes keep fallback paths.
export const avatarShapePaths: Record<AvatarShape, string> = {
  blob: "M 43.3 7.0 C 48.6 6.3 55.3 6.5 60.7 7.8 C 66.1 9.1 71.3 11.8 75.6 14.9 C 79.9 18.1 83.9 22.5 86.7 26.7 C 89.5 30.9 91.3 34.6 92.2 40.1 C 93.1 45.6 93.5 53.8 92.2 59.9 C 90.9 66.0 87.7 71.8 84.3 76.4 C 80.9 81.0 76.4 84.7 71.7 87.5 C 67.0 90.3 61.2 92.2 55.9 93.0 C 50.6 93.8 45.4 93.5 40.1 92.2 C 34.9 90.9 28.9 88.2 24.4 85.1 C 19.9 81.9 16.2 78.0 13.3 73.3 C 10.4 68.6 7.9 62.4 7.0 56.7 C 6.1 51.0 6.5 44.7 7.8 39.3 C 9.1 33.9 11.3 29.0 14.9 24.4 C 18.4 19.8 24.4 14.6 29.1 11.7 C 33.8 8.8 38.0 7.7 43.3 7.0 Z",
  pebble: "M50 20c16-6 30 4 32 20 2 18-8 34-24 38-18 4-34-8-36-26C20 32 32 26 50 20Z",
  bean: "M34 22c16-8 34 0 36 16 2 18-8 24-6 36-8 12-28 14-38 2-12-14 2-22 0-36 0-8 4-14 8-18Z",
  egg: "M50 14c14 2 26 16 26 34 0 20-12 36-26 36S24 68 24 48C24 30 36 16 50 14Z",
  squircle: "M28 18h44c10 0 16 8 16 18v28c0 12-8 18-18 18H30c-12 0-18-8-18-18V36c0-10 6-18 16-18Z",
  tablet: "M18 28h64c8 0 12 6 12 14v16c0 10-6 14-14 14H20c-8 0-14-6-14-14V42c0-8 4-14 12-14Z",
  capsule: "M 61.7 7.0 C 69.6 5.9 71.5 8.2 74.8 9.2 C 78.1 10.2 79.0 11.0 81.3 12.8 C 83.6 14.6 86.6 17.2 88.6 20.1 C 90.5 23.0 92.3 26.3 93.0 30.3 C 93.7 34.3 93.6 40.4 93.0 44.2 C 92.4 48.0 91.7 49.8 89.4 52.9 C 87.1 56.0 85.5 59.9 79.2 63.1 C 72.9 66.3 57.6 70.1 51.5 71.9 C 45.4 73.7 46.1 73.7 42.7 74.1 C 39.3 74.5 34.6 74.7 31.1 74.1 C 27.6 73.5 24.8 72.5 21.6 70.4 C 18.4 68.3 14.4 64.7 12.1 61.7 C 9.8 58.7 8.6 55.7 7.7 52.2 C 6.9 48.7 6.5 44.3 7.0 40.5 C 7.5 36.7 8.9 32.8 10.6 29.6 C 12.3 26.5 14.4 23.9 17.2 21.6 C 20.0 19.3 20.0 18.1 27.4 15.7 C 34.8 13.3 53.8 8.1 61.7 7.0 Z",
  cylinder: "M24 28h52c8 0 12 4 12 10v28c0 8-6 12-14 12H26c-8 0-14-4-14-12V38c0-6 4-10 12-10Zm0 8c8 4 36 4 52 0",
  hex: "M 35.7 7.0 L 43.2 7.0 L 68.9 13.8 L 76.4 17.6 L 92.2 47.7 L 93.0 58.3 L 91.5 61.3 L 73.4 83.9 L 64.3 90.7 L 37.9 85.5 L 24.4 80.2 L 7.8 48.5 L 7.0 40.9 L 10.0 34.9 L 24.4 16.8 Z",
  gem: "M50 12 82 40 50 88 18 40 50 12Zm0 0 32 28H18L50 12Z",
  crystal: "M50 10 68 34 78 28 70 52 84 70 50 90 16 70 30 52 22 28 32 34 50 10Z",
  wedge: "M 24.0 7.0 L 33.9 7.9 L 44.6 13.3 L 81.4 37.5 L 90.3 47.3 L 90.3 58.1 L 77.8 72.4 L 54.5 85.8 L 33.0 93.0 L 16.9 93.0 L 9.7 87.6 L 7.0 81.4 L 12.4 21.3 L 17.8 10.6 Z",
  shield: "M50 12 82 26v26c0 20-14 32-32 38-18-6-32-18-32-38V26L50 12Z",
  dome: "M18 52h64v22c0 8-6 12-14 12H32c-8 0-14-4-14-12V52Zm0 0c0-18 14-32 32-32s32 14 32 32",
  arch: "M22 84V48c0-16 12-30 28-30s28 14 28 30v36H22Z",
  cloud: "M 54.3 7.0 C 56.5 6.6 59.0 6.5 61.4 7.0 C 63.8 7.5 65.6 7.7 68.5 10.2 C 71.4 12.7 75.2 18.6 78.8 22.0 C 82.3 25.4 87.4 27.9 89.8 30.7 C 92.2 33.5 92.6 34.8 93.0 38.6 C 93.4 42.4 93.7 49.4 92.2 53.6 C 90.8 57.8 87.6 61.2 84.3 63.8 C 81.0 66.4 76.8 66.7 72.5 69.3 C 68.2 71.9 62.1 77.6 58.3 79.6 C 54.5 81.6 52.6 81.3 49.6 81.2 C 46.6 81.1 42.7 78.8 40.1 78.8 C 37.5 78.8 36.8 80.9 33.8 81.2 C 30.8 81.5 25.8 82.0 22.0 80.4 C 18.2 78.8 13.3 74.3 10.9 71.7 C 8.5 69.1 8.4 67.1 7.8 64.6 C 7.1 62.1 6.6 59.3 7.0 56.7 C 7.4 54.1 9.9 50.9 10.2 48.8 C 10.5 46.7 8.6 47.9 8.6 44.1 C 8.6 40.3 8.4 30.9 10.2 25.9 C 12.0 20.9 16.9 16.7 19.6 14.1 C 22.4 11.5 23.8 11.2 26.7 10.2 C 29.6 9.1 33.5 7.9 37.0 7.8 C 40.5 7.7 45.1 9.5 48.0 9.4 C 50.9 9.3 52.1 7.4 54.3 7.0 Z",
  teardrop: "M 25.0 7.0 C 27.9 6.3 28.9 5.3 35.9 7.8 C 42.9 10.3 60.4 18.2 67.2 21.9 C 74.0 25.5 73.7 26.2 76.6 29.7 C 79.5 33.2 83.0 36.9 84.4 43.0 C 85.8 49.1 85.3 61.7 85.2 66.4 C 85.1 71.1 84.6 68.9 83.6 71.1 C 82.5 73.3 81.1 76.8 78.9 79.7 C 76.7 82.6 73.0 86.1 70.3 88.3 C 67.6 90.5 69.1 92.2 62.5 93.0 C 55.9 93.8 37.7 94.2 30.5 93.0 C 23.3 91.8 22.9 89.2 19.5 86.0 C 16.1 82.8 12.2 77.2 10.1 73.5 C 8.0 69.8 7.5 68.3 7.0 64.1 C 6.5 59.9 5.6 56.2 7.0 48.4 C 8.4 40.6 13.7 23.3 15.6 17.2 C 17.6 11.1 17.1 13.4 18.7 11.7 C 20.3 10.0 22.1 7.7 25.0 7.0 Z",
  leaf: "M50 14c22 8 34 28 30 48-6 18-24 28-40 24-8-20-6-46 10-72Z",
  square: "M 60.9 7.0 L 66.8 7.7 L 70.4 11.4 L 93.0 58.7 L 93.0 66.0 L 84.3 74.1 L 36.2 86.4 L 30.3 82.1 L 9.2 38.3 L 7.0 29.6 L 8.5 25.9 L 17.9 18.7 Z",
  sparkle: "M 44.5 7.0 C 47.2 5.2 47.2 6.5 48.9 7.0 C 50.6 7.5 52.6 7.9 54.8 9.9 C 57.0 11.9 59.9 15.4 62.1 18.8 C 64.3 22.2 64.5 27.8 68.0 30.5 C 71.5 33.2 79.9 33.5 83.4 34.9 C 87.0 36.2 87.7 37.1 89.3 38.6 C 90.9 40.1 92.5 41.2 93.0 43.8 C 93.5 46.4 93.8 50.8 92.3 54.0 C 90.8 57.2 87.4 60.4 84.2 62.9 C 81.0 65.3 75.7 65.5 73.2 68.7 C 70.8 71.9 71.6 78.1 69.5 82.0 C 67.4 85.9 64.5 90.7 60.7 92.3 C 56.9 93.9 50.3 93.1 46.7 91.5 C 43.1 89.9 41.5 86.1 39.3 82.7 C 37.1 79.3 37.6 74.0 33.5 70.9 C 29.4 67.8 18.8 67.0 14.4 64.3 C 10.0 61.6 7.6 58.0 7.0 54.8 C 6.4 51.6 8.9 48.0 10.7 45.2 C 12.5 42.4 15.1 40.1 18.0 37.9 C 20.9 35.7 25.9 35.3 28.3 32.0 C 30.8 28.7 30.0 22.2 32.7 18.0 C 35.4 13.8 41.8 8.8 44.5 7.0 Z",
  clover: "M 52.3 7.0 C 55.9 6.9 62.1 7.5 66.1 9.3 C 70.1 11.1 73.7 14.9 76.1 17.8 C 78.5 20.8 79.9 23.9 80.7 27.0 C 81.5 30.1 79.4 33.5 80.7 36.2 C 82.0 38.9 86.4 40.4 88.4 43.1 C 90.5 45.8 92.2 48.8 93.0 52.3 C 93.8 55.8 93.4 61.0 93.0 63.8 C 92.6 66.6 92.5 66.8 90.7 69.2 C 88.9 71.6 85.2 76.2 82.2 78.4 C 79.2 80.6 76.5 81.8 73.0 82.2 C 69.5 82.6 64.8 79.8 61.5 80.7 C 58.2 81.6 56.3 86.2 53.1 87.6 C 49.9 89.0 45.8 89.6 42.3 89.2 C 38.8 88.8 35.4 87.2 32.3 85.3 C 29.2 83.4 25.9 80.3 23.9 77.6 C 21.9 74.9 20.7 72.1 20.1 69.2 C 19.5 66.3 21.7 63.1 20.1 60.0 C 18.6 56.9 13.0 54.0 10.8 50.8 C 8.6 47.6 7.6 43.5 7.0 40.8 C 6.4 38.1 6.4 37.2 7.0 34.6 C 7.6 32.0 8.8 28.3 10.8 25.4 C 12.9 22.5 16.7 18.9 19.3 17.0 C 21.9 15.1 22.8 14.3 26.2 13.9 C 29.6 13.5 36.9 15.3 40.0 14.7 C 43.1 14.1 42.6 11.4 44.6 10.1 C 46.6 8.8 48.7 7.1 52.3 7.0 Z",
  flower: "M 38.9 7.0 C 42.2 7.1 48.5 9.1 51.5 10.7 C 54.5 12.3 54.0 16.1 56.7 16.6 C 59.4 17.1 64.5 13.4 67.8 13.7 C 71.1 13.9 74.2 15.6 76.7 18.1 C 79.2 20.6 81.9 25.4 82.6 28.5 C 83.3 31.6 79.9 34.2 81.1 36.7 C 82.3 39.2 88.0 41.2 90.0 43.3 C 92.0 45.4 92.8 46.1 93.0 49.3 C 93.2 52.5 93.7 58.5 91.5 62.6 C 89.3 66.7 81.9 71.0 79.7 73.7 C 77.5 76.4 79.6 76.7 78.2 78.9 C 76.8 81.1 73.8 85.0 71.5 87.1 C 69.2 89.2 67.1 90.9 64.1 91.5 C 61.1 92.1 57.0 92.3 53.7 90.8 C 50.4 89.3 47.3 83.5 44.1 82.6 C 40.9 81.7 37.7 85.8 34.4 85.6 C 31.1 85.3 26.8 83.4 24.1 81.1 C 21.4 78.8 18.9 74.6 18.1 71.5 C 17.4 68.4 20.8 65.2 19.6 62.6 C 18.4 60.0 12.8 58.2 10.7 55.9 C 8.6 53.5 7.6 51.0 7.0 48.5 C 6.4 46.0 6.4 43.5 7.0 41.1 C 7.6 38.8 8.3 37.1 10.7 34.4 C 13.0 31.7 19.0 27.4 21.1 24.8 C 23.2 22.2 21.6 21.4 23.3 18.9 C 25.0 16.4 28.9 12.0 31.5 10.0 C 34.1 8.0 35.6 6.9 38.9 7.0 Z",
  heart: "M 60.0 7.0 C 64.0 5.7 65.6 6.3 68.9 7.0 C 72.2 7.7 76.9 9.1 80.1 11.0 C 83.3 12.9 86.0 15.5 88.2 18.3 C 90.4 21.1 92.2 24.0 93.0 27.9 C 93.8 31.8 95.3 33.8 93.0 41.6 C 90.7 49.4 82.8 67.7 79.3 74.5 C 75.8 81.3 74.8 80.7 72.1 82.6 C 69.4 84.5 66.2 85.4 63.3 85.8 C 60.3 86.2 61.9 88.2 54.4 85.0 C 46.9 81.8 25.5 70.9 18.3 66.5 C 11.1 62.1 12.9 61.5 11.0 58.4 C 9.1 55.3 7.4 52.3 7.0 48.0 C 6.6 43.7 6.7 37.2 8.6 32.7 C 10.5 28.2 14.7 23.6 18.3 20.7 C 21.9 17.8 25.9 15.9 30.3 15.0 C 34.7 14.1 39.8 16.3 44.8 15.0 C 49.8 13.7 56.0 8.3 60.0 7.0 Z",
  house: "M 33.2 7.0 L 64.3 17.6 L 75.0 25.0 L 93.0 61.1 L 93.0 69.2 L 85.6 77.4 L 34.8 92.2 L 27.5 91.4 L 23.4 87.3 L 7.0 52.9 L 7.0 42.2 L 20.1 17.6 L 27.5 9.5 Z",
  star: "M 35.7 7.0 L 42.3 8.5 L 54.8 21.0 L 59.2 21.0 L 68.7 15.8 L 76.1 18.0 L 78.3 21.7 L 76.1 37.9 L 79.0 41.5 L 89.3 47.4 L 93.0 52.6 L 93.0 58.5 L 86.4 66.5 L 74.6 69.5 L 75.4 79.8 L 73.2 84.2 L 65.8 90.8 L 57.7 90.1 L 46.7 78.3 L 43.0 76.8 L 30.5 82.7 L 23.9 80.5 L 21.7 77.6 L 23.9 60.7 L 7.0 46.7 L 7.7 37.9 L 14.4 31.3 L 25.4 28.3 L 25.4 16.6 L 31.3 9.2 Z",
};

function shapePath(shape: AvatarShape): ReactNode {
  return <path d={avatarShapePaths[shape]} />;
}

export function PuckAvatar({ assistantId, name, state = "idle", design, size = "medium", attention = false, animated = true }: { assistantId: string; name: string; state?: PuckState; design?: AvatarDesign; size?: "small" | "medium" | "large"; attention?: boolean; animated?: boolean }) {
  const instanceId = useId().replaceAll(":", "");
  const root = useRef<HTMLDivElement>(null);
  const reducedMotion = useRef(false);
  const restPose = useMemo(() => restPoseForKey(`${assistantId}:${instanceId}`), [assistantId, instanceId]);
  const currentPose = useRef(restPose);
  const avatar = useMemo(() => design || deriveAvatar(assistantId), [assistantId, design]);
  const expression = expressionForState(state);

  useEffect(() => {
    if (!animated || !root.current) return;
    const element = root.current;
    const countMotion = () => element.dataset.motionCount = String(Number(element.dataset.motionCount || 0) + 1);
    const target = {
      setBlinkPhase(phase: BlinkPhase) { element.dataset.blinkPhase = phase; countMotion(); },
      setPose(pose: AvatarPose) {
        currentPose.current = pose;
        applyPose(element, pose);
        countMotion();
      },
    };
    const media = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-reduced-motion: reduce)") : undefined;
    reducedMotion.current = media?.matches ?? false;
    element.dataset.reducedMotion = String(reducedMotion.current);
    const registration = registerAvatarMotion(`${assistantId}:${instanceId}`, target, { reducedMotion: reducedMotion.current });
    const mediaChanged = () => {
      reducedMotion.current = media?.matches ?? false;
      element.dataset.reducedMotion = String(reducedMotion.current);
      registration.setReducedMotion(reducedMotion.current);
    };
    media?.addEventListener("change", mediaChanged);
    const observer = typeof IntersectionObserver === "function" ? new IntersectionObserver(records => {
      const record = records[0];
      if (record) registration.setVisible(record.isIntersecting);
    }) : undefined;
    observer?.observe(element);
    return () => {
      observer?.disconnect();
      media?.removeEventListener("change", mediaChanged);
      registration.unregister();
    };
  }, [animated, assistantId, instanceId]);

  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const moving = new Set<EventTarget>();
    const transitionStarted = (event: Event) => {
      if ((event as TransitionEvent).propertyName !== "transform") return;
      moving.add(event.target!);
      element.dataset.moving = "true";
    };
    const transitionFinished = (event: Event) => {
      if ((event as TransitionEvent).propertyName !== "transform") return;
      moving.delete(event.target!);
      if (moving.size === 0) element.dataset.moving = "false";
    };
    element.addEventListener("transitionrun", transitionStarted);
    element.addEventListener("transitionend", transitionFinished);
    element.addEventListener("transitioncancel", transitionFinished);
    return () => {
      element.removeEventListener("transitionrun", transitionStarted);
      element.removeEventListener("transitionend", transitionFinished);
      element.removeEventListener("transitioncancel", transitionFinished);
    };
  }, []);

  function follow(event: PointerEvent<HTMLDivElement>) {
    if (reducedMotion.current || event.pointerType === "touch" || !root.current) return;
    const box = root.current.getBoundingClientRect();
    const x = Math.max(-1, Math.min(1, (event.clientX - box.left) / box.width * 2 - 1));
    const y = Math.max(-1, Math.min(1, (event.clientY - box.top) / box.height * 2 - 1));
    applyPose(root.current, {
      name: "pointer", motionDuration: 720, headX: x * 7.5, headY: y * 6.8, headRotate: Math.abs(x) > .12 ? x * 24 : y * 20,
      faceX: x * 11.5, faceY: y * 10, faceRotate: Math.abs(x) > .12 ? x * 28 + y * 4 : currentPose.current.faceRotate,
      leftEyeScaleX: x < -.12 ? .55 : 1, rightEyeScaleX: x > .12 ? .55 : 1,
      leftEyeScaleY: x < -.12 ? .78 : 1, rightEyeScaleY: x > .12 ? .78 : 1, eyeRoundness: 1.7,
    });
  }

  function react() {
    const element = root.current;
    if (!element || reducedMotion.current) return;
    element.dataset.reacting = "true";
    const animations = Array.from(element.querySelectorAll<HTMLElement>(".puck-avatar-sparkle")).flatMap((sparkle, index) => typeof sparkle.animate === "function" ? [sparkle.animate([
      { opacity: 0, transform: "translate(-50%, -50%) scale(.2)" },
      { opacity: 1, offset: .28, transform: `translate(calc(-50% + ${index % 2 ? 16 : -16}px), calc(-50% - ${12 + index * 2}px)) scale(1)` },
      { opacity: 0, transform: `translate(calc(-50% + ${index % 2 ? 22 : -22}px), calc(-50% - ${18 + index * 3}px)) scale(.4)` },
    ], { duration: 420, easing: "ease-out" })] : []);
    const winkTarget = element.querySelector<SVGElement>(".puck-avatar-eye-left .puck-avatar-eye-shape");
    const wink = winkTarget && typeof winkTarget.animate === "function" ? winkTarget.animate([
      { transform: "scaleY(1)" }, { transform: "scaleY(.05)", offset: .45 }, { transform: "scaleY(1)" },
    ], { duration: 300, easing: "ease-out" }) : undefined;
    if (wink) animations.push(wink);
    if (animations.length === 0) { element.dataset.reacting = "false"; return; }
    void Promise.allSettled(animations.map(animation => animation.finished)).then(() => { if (root.current === element) element.dataset.reacting = "false"; });
  }

  const style = {
    "--puck-avatar-color": avatar.colorHex,
    "--puck-motion-duration": `${restPose.motionDuration}ms`,
    "--puck-head-x": `${restPose.headX}%`, "--puck-head-y": `${restPose.headY}%`, "--puck-head-rotate": `${restPose.headRotate}deg`,
    "--puck-face-x": `${restPose.faceX}px`, "--puck-face-y": `${restPose.faceY}px`, "--puck-face-rotate": `${restPose.faceRotate}deg`,
    "--puck-left-eye-xscale": restPose.leftEyeScaleX, "--puck-right-eye-xscale": restPose.rightEyeScaleX,
    "--puck-left-eye-yscale": restPose.leftEyeScaleY, "--puck-right-eye-yscale": restPose.rightEyeScaleY,
    "--puck-eye-roundness": restPose.eyeRoundness,
  } as CSSProperties;

  return <div ref={root} className={`puck-avatar puck-avatar-${size}`} role="img" aria-label={`${name}，${stateLabel(state)}`} draggable={false}
    data-expression={expression} data-shape={avatar.shape} data-reacting="false" data-moving="false" data-blink-phase="open" data-pose="rest" data-motion-count="0"
    style={style} onPointerMove={follow} onPointerLeave={() => { if (root.current) applyPose(root.current, currentPose.current); }}
    onPointerDown={react} onDragStart={event => event.preventDefault()}>
    {avatar.image ? <img className="puck-avatar-photo" src={avatar.image} alt="" /> : <span className="puck-avatar-motion"><svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
      <g className="puck-avatar-head">
      <g className="puck-avatar-body">{shapePath(avatar.shape)}</g>
      <g className="puck-avatar-face">
        <g className="puck-avatar-eyes" transform="translate(10 -8) rotate(-18 44 42)">
          <g className="puck-avatar-eye puck-avatar-eye-left"><rect className="puck-avatar-eye-shape" x="34" y="38" width="7" height="14" rx="3.5"/></g>
          <g className="puck-avatar-eye puck-avatar-eye-right"><rect className="puck-avatar-eye-shape" x="44" y="38" width="7" height="14" rx="3.5"/></g>
        </g>
      </g>
      </g>
    </svg></span>}
    <span className="puck-avatar-sparkles" aria-hidden="true">{Array.from({ length: 5 }, (_, index) => <i className="puck-avatar-sparkle" key={index}/>)}</span>
    {attention && <span className="puck-avatar-badge" aria-hidden="true" />}
  </div>;
}
