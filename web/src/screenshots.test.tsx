// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConversationImage, readScreenshotFiles } from "./screenshots";

describe("conversation screenshots", () => {
  it("accepts PNG and JPEG within the total cap but rejects SVG and oversized files", async () => {
    const png = new File([Uint8Array.from([137, 80, 78, 71, 13, 10, 26, 10])], "shot.png", { type: "image/png" });
    const result = await readScreenshotFiles([], [png]);
    expect(result).toHaveLength(1);
    expect(result[0].dataUri).toMatch(/^data:image\/png;base64,/);
    await expect(readScreenshotFiles([], [new File(["<svg/>"] , "x.svg", { type: "image/svg+xml" })])).rejects.toThrow();
    await expect(readScreenshotFiles([], [new File([new Uint8Array((4 << 20) + 1)], "big.png", { type: "image/png" })])).rejects.toThrow();
  });

  it("loads by explicit action, displays a responsive image, and can enlarge it", async () => {
    const load = vi.fn().mockResolvedValue("data:image/png;base64,AAAA");
    render(<ConversationImage alt="result" load={load} />);
    fireEvent.click(screen.getByRole("button", { name: "加载截图：result" }));
    await waitFor(() => expect(screen.getByRole("img", { name: "result" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "放大截图：result" }));
    expect(screen.getByRole("dialog", { name: "result" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "关闭截图" }));
    expect(screen.queryByRole("dialog", { name: "result" })).toBeNull();
  });

  it("keeps a retry control and explains an image load failure", async () => {
    const load = vi.fn().mockRejectedValueOnce(new Error("图片格式、尺寸或内容不受支持")).mockResolvedValueOnce("data:image/jpeg;base64,AAAA");
    render(<ConversationImage alt="failed" load={load} />);
    fireEvent.click(screen.getByRole("button", { name: "加载截图：failed" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "重试加载截图：failed" })).toBeTruthy());
    expect(screen.getByText("截图未能加载：图片格式、尺寸或内容不受支持。点此重试")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试加载截图：failed" }));
    await waitFor(() => expect(screen.getByRole("img", { name: "failed" })).toBeTruthy());
  });
});
