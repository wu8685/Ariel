// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MermaidDiagram } from "./MermaidDiagram";

const mermaid = vi.hoisted(() => ({ initialize: vi.fn(), render: vi.fn() }));
vi.mock("mermaid", () => ({ default: mermaid }));

function svg(label: string) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 40"><text>${label}</text></svg>`;
}

afterEach(cleanup);
beforeEach(() => {
  mermaid.render.mockReset();
  mermaid.render.mockImplementation(async (_id: string, source: string) => ({ svg: svg(source) }));
});

describe("Mermaid diagram rendering", () => {
  it("shows source instead of injecting unsafe SVG", async () => {
    mermaid.render.mockResolvedValueOnce({ svg: '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>' });
    render(<MermaidDiagram source="flowchart LR\nA --> B" />);
    expect(await screen.findByText("图表未通过安全检查，已显示源码。")).toBeTruthy();
    expect(screen.getByText(/flowchart LR/)).toBeTruthy();
    expect(document.querySelector("script")).toBeNull();
  });

  it("keeps the newest render when an older streaming render finishes late", async () => {
    let finishOld!: (value: { svg: string }) => void;
    let finishNew!: (value: { svg: string }) => void;
    mermaid.render
      .mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve; }))
      .mockImplementationOnce(() => new Promise(resolve => { finishNew = resolve; }));
    const view = render(<MermaidDiagram source="flowchart LR\nA" />);
    await waitFor(() => expect(mermaid.render).toHaveBeenCalledTimes(1));
    view.rerender(<MermaidDiagram source="flowchart LR\nA --> B" />);
    await waitFor(() => expect(mermaid.render).toHaveBeenCalledTimes(2));
    finishNew({ svg: svg("new") });
    expect(await screen.findByText("new")).toBeTruthy();
    finishOld({ svg: svg("old") });
    await Promise.resolve();
    expect(screen.queryByText("old")).toBeNull();
    expect(screen.getByText("new")).toBeTruthy();
  });

  it("bounds oversized diagrams before loading the renderer", async () => {
    render(<MermaidDiagram source={`flowchart LR\nA[${"x".repeat(33 * 1024)}]`} />);
    expect(await screen.findByText("图表内容过大，已显示源码。")).toBeTruthy();
    expect(mermaid.render).not.toHaveBeenCalled();
  });

  it("retains source when rendering rejects", async () => {
    mermaid.render.mockRejectedValueOnce(new Error("parse failure"));
    render(<MermaidDiagram source="flowchart LR\nA --" />);
    expect(await screen.findByText("图表暂时无法渲染，已显示源码。")).toBeTruthy();
    expect(screen.getByText(/A --/)).toBeTruthy();
  });
});
