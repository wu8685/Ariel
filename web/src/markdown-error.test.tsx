// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ConversationMarkdown } from "./markdown";

vi.mock("react-markdown", () => ({ default: () => { throw new Error("synthetic parser failure"); } }));

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("keeps the entire message as plain text if Markdown rendering fails", () => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  const original = "**important** <script>still just text</script>";
  render(<ConversationMarkdown text={original} />);
  expect(screen.getByText("Markdown 无法解析，已改为纯文本显示。")).toBeTruthy();
  expect(document.querySelector(".markdown-fallback-text")?.textContent).toBe(original);
  expect(document.querySelector("script, strong")).toBeNull();
});
