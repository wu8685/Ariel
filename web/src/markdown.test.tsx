// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ConversationMarkdown } from "./markdown";

afterEach(cleanup);

describe("conversation Markdown projection", () => {
  it("renders CommonMark and GFM as semantic, readable chat content", () => {
    const text = [
      "## Heading", "", "**bold** and *emphasis* and ~~removed~~ with `inline`.", "",
      "> quoted", "", "- first", "- [x] done", "", "1. ordered", "", "---", "",
      "| Name | Value |", "| --- | --- |", "| key | value |", "",
      "```go", "fmt.Println(\"hello\")", "```", "",
      "A footnote[^1].", "", "[^1]: Note text.",
    ].join("\n");
    render(<ConversationMarkdown text={text} />);
    expect(screen.getByRole("heading", { name: "Heading", level: 2 })).toBeTruthy();
    expect(screen.getByText("bold").tagName).toBe("STRONG");
    expect(screen.getByText("emphasis").tagName).toBe("EM");
    expect(screen.getByText("removed").tagName).toBe("DEL");
    expect(screen.getByText("inline").tagName).toBe("CODE");
    expect(screen.getByText("quoted").closest("blockquote")).toBeTruthy();
    expect(screen.getByText("first").closest("li")).toBeTruthy();
    expect(screen.getByRole("checkbox").hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("table").querySelector("td")?.textContent).toBe("key");
    expect(screen.getByText(/fmt\.Println/).closest("pre")).toBeTruthy();
    expect(screen.getByText(/Note text\./)).toBeTruthy();
  });

  it("makes only explicit safe links navigable without automatic requests", () => {
    render(<ConversationMarkdown text={'[web](https://example.test/path) [mail](mailto:me@example.test) [local](#note) [script](javascript:alert(1)) [data](data:text/html,evil) [relative](../private) [protocol-relative](//example.test/x) [credentials](https://user:pass@example.test/)'} />);
    const web = screen.getByRole("link", { name: "web" });
    expect(web.getAttribute("href")).toBe("https://example.test/path");
    expect(web.getAttribute("target")).toBe("_blank");
    expect(web.getAttribute("rel")).toContain("noopener");
    expect(web.getAttribute("rel")).toContain("noreferrer");
    expect(screen.getByRole("link", { name: "mail" }).getAttribute("href")).toBe("mailto:me@example.test");
    expect(screen.getByRole("link", { name: "local" }).getAttribute("href")).toBe("#note");
    for (const name of ["script", "data", "relative", "protocol-relative", "credentials"]) {
      expect(screen.getByText(name).closest("a")).toBeNull();
    }
  });

  it("does not render raw HTML or auto-fetch Markdown images", () => {
    render(<ConversationMarkdown text={'Visible <script>alert(1)</script> <iframe src="https://example.test/"></iframe>\n\n![diagram](https://example.test/track.png) ![](data:image/png;base64,abc)'} />);
    expect(document.querySelector("script, iframe, img, video, audio, object, embed")).toBeNull();
    expect(screen.getByText("图片：diagram")).toBeTruthy();
    expect(screen.getByText("图片", { exact: true })).toBeTruthy();
  });

  it("retains the entire raw body with an explicit notice when one item is too large to parse", () => {
    const text = "a".repeat(256 * 1024 + 1);
    render(<ConversationMarkdown text={text} />);
    expect(screen.getByText(/内容过长，暂以纯文本显示/)).toBeTruthy();
    expect(document.querySelector(".markdown-fallback-text")?.textContent).toBe(text);
  });

  it("keeps unfinished Markdown readable during streaming", () => {
    render(<ConversationMarkdown text={'**unfinished\n\n```go\nfmt.Println(1)'} />);
    expect(document.querySelector(".markdown-body")?.textContent).toContain("unfinished");
    expect(document.querySelector(".markdown-body")?.textContent).toContain("fmt.Println(1)");
  });

  it("retains single chat line breaks inside a paragraph", () => {
    render(<ConversationMarkdown text={"first line\nsecond line"} />);
    expect(document.querySelector(".markdown-body p")?.textContent).toBe("first line\nsecond line");
  });

  it("keeps footnote targets distinct across different messages", () => {
    const text = "A note[^1].\n\n[^1]: The note.";
    render(<><ConversationMarkdown text={text} /><ConversationMarkdown text={text} /></>);
    const references = [...document.querySelectorAll<HTMLAnchorElement>('a[data-footnote-ref]')];
    expect(references).toHaveLength(2);
    expect(references[0].getAttribute("href")).not.toBe(references[1].getAttribute("href"));
    for (const reference of references) {
      const target = reference.getAttribute("href")?.slice(1);
      expect(target && document.getElementById(target)).toBeTruthy();
    }
  });
});
