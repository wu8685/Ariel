// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
afterEach(cleanup);
import { CircleButton, CloseGlyph } from "./circular-button";
describe("shared circular button",()=>{
 it("preserves native accessibility and fires once from the actual button",()=>{
  const click=vi.fn();render(<CircleButton aria-label="关闭" onClick={click}><CloseGlyph/></CircleButton>);
  const button=screen.getByRole("button",{name:"关闭"});expect(button.classList.contains("circle-control")).toBe(true);expect(button.querySelector(".circle-visual svg")).toBeTruthy();fireEvent.click(button);expect(click).toHaveBeenCalledTimes(1);
 });
 it("preserves disabled and loading geometry",()=>{const click=vi.fn();render(<CircleButton aria-label="发送" disabled onClick={click}><svg className="send-loading-spinner"/></CircleButton>);fireEvent.click(screen.getByRole("button"));expect(click).not.toHaveBeenCalled();expect(document.querySelector(".circle-visual .send-loading-spinner")).toBeTruthy();});
});
