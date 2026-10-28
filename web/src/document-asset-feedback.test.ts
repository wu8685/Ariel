import { expect, it } from "vitest";
import { documentAssetErrorText } from "./document-assets";

it.each([
  [
    "UNSUPPORTED_FILE_TYPE",
    "图片格式不受支持或文件已损坏；当前支持 PNG、JPEG、GIF、WebP。",
  ],
  [
    "FILE_TOO_LARGE",
    "图片超过大小、尺寸或复杂度限制（最大 4 MiB、单边 8192 px、1200 万像素）。",
  ],
  ["FILE_NOT_FOUND", "图片文件已不存在，请检查文档中的图片引用。"],
  ["PATH_NOT_ALLOWED", "这项图片引用不允许读取。"],
  ["FILE_CHANGED", "文档或图片已经变化，请重新打开文档。"],
  ["FEATURE_DISABLED", "当前设备尚未启用文档图片读取。"],
  ["TIMEOUT", "图片读取超时或分块已过期，请重试图片。"],
  ["OVERLOADED", "等待加载的图片过多，请稍后重试图片。"],
  ["PROTOCOL_UNSUPPORTED", "当前设备的文档图片能力暂不可用，请重新打开文档。"],
  ["UNKNOWN_BACKEND_DETAIL", "图片暂时无法读取，请重试。"],
  [undefined, "图片暂时无法读取，请重试。"],
])(
  "maps document image failure %s without borrowing text-file constraints",
  (code, expected) => {
    expect(documentAssetErrorText(code)).toBe(expected);
  },
);

it("only advertises static SVG in image failures after capability negotiation", () => {
  expect(documentAssetErrorText("UNSUPPORTED_FILE_TYPE", true)).toBe(
    "图片格式不受支持、文件已损坏或 SVG 不满足静态安全要求；当前支持 PNG、JPEG、GIF、WebP 和受限静态 SVG。",
  );
  expect(documentAssetErrorText("UNSUPPORTED_FILE_TYPE", false)).not.toContain(
    "SVG",
  );
});
