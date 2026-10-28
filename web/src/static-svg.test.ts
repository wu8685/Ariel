// @vitest-environment jsdom
import { expect, it } from "vitest";
import { staticSVGInfo } from "./static-svg";

it.each(["path", "text"])(
  "bounds expanded %s rendering work, not only SVG element count",
  (kind) => {
    let inner =
      kind === "path"
        ? `<path id="p0" d="M0 0 ${"L1 1 ".repeat(1000)}"/>`
        : `<text id="p0">${"x".repeat(2048)}</text>`;
    for (let i = 1; i <= 10; i++)
      inner += `<g id="p${i}"><use href="#p${i - 1}"/><use href="#p${i - 1}"/></g>`;
    const source = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><defs>${inner}</defs><use href="#p10"/></svg>`;
    expect(() => staticSVGInfo(new TextEncoder().encode(source))).toThrow();
  },
);
