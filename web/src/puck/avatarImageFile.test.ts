// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { avatarImage } from "./PuckAvatar";
import { readAvatarImageFile } from "./avatarImageFile";

const pngBytes = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="), char => char.charCodeAt(0));

describe("readAvatarImageFile", () => {
  it("keeps a small png and rejects other types", async () => {
    const stored = await readAvatarImageFile(new File([pngBytes], "avatar.png", { type: "image/png" }));
    expect(stored).toMatch(/^data:image\/png;base64,/);
    expect(avatarImage(stored)).toBe(stored);
    await expect(readAvatarImageFile(new File(["<svg/>"], "avatar.svg", { type: "image/svg+xml" }))).rejects.toThrow("仅支持 PNG、JPEG 或 WebP。");
  });

  it("rejects a file that is too large to decode into the avatar field", async () => {
    await expect(readAvatarImageFile(new File([new Uint8Array(300_000)], "big.png", { type: "image/png" }))).rejects.toThrow(/无法读取这张图片|图片仍然太大|无法处理这张图片/);
  });
});