// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
afterEach(cleanup);
import { DevicePicker } from "./device-picker";
const devices = [{ deviceId:"a",deviceName:"First Mac",agentOnline:true },{deviceId:"b",deviceName:"Second Mac",agentOnline:false}];
describe("themed device picker",()=>{
 it("navigates without switching until committed and does not reselect the current device",()=>{
  const change=vi.fn();render(<DevicePicker devices={devices} value="a" disabled={false} visible onChange={change}/>);
  const trigger=screen.getByRole("combobox",{name:"设备"});fireEvent.click(trigger);
  expect(screen.getByRole("option",{name:"First Mac"}).getAttribute("aria-selected")).toBe("true");
  fireEvent.keyDown(trigger,{key:"ArrowDown"});expect(change).not.toHaveBeenCalled();fireEvent.keyDown(trigger,{key:"Enter"});expect(change).toHaveBeenCalledExactlyOnceWith("b");
  fireEvent.click(trigger);fireEvent.click(screen.getByRole("option",{name:"First Mac"}));expect(change).toHaveBeenCalledTimes(1);
 });
 it("closes on escape, outside click, hidden sidebar and disabled transition",()=>{
  const props={devices,value:"a",disabled:false,visible:true,onChange:vi.fn()};const {rerender}=render(<DevicePicker {...props}/>);const trigger=screen.getByRole("combobox");
  fireEvent.click(trigger);fireEvent.keyDown(trigger,{key:"Escape"});expect(screen.queryByRole("listbox")).toBeNull();
  fireEvent.click(trigger);fireEvent.pointerDown(document.body);expect(screen.queryByRole("listbox")).toBeNull();
  fireEvent.click(trigger);rerender(<DevicePicker {...props} visible={false}/>);expect(screen.queryByRole("listbox")).toBeNull();
  rerender(<DevicePicker {...props}/>);fireEvent.click(trigger);rerender(<DevicePicker {...props} disabled/>);expect(screen.queryByRole("listbox")).toBeNull();
 });
 it("handles options disappearing and an empty list without submitting invalid IDs",()=>{
  const change=vi.fn();const {rerender}=render(<DevicePicker devices={devices} value="a" disabled={false} visible onChange={change}/>);const trigger=screen.getByRole("combobox");fireEvent.click(trigger);fireEvent.keyDown(trigger,{key:"End"});
  rerender(<DevicePicker devices={[devices[0]]} value="a" disabled={false} visible onChange={change}/>);fireEvent.keyDown(trigger,{key:"Enter"});expect(change).not.toHaveBeenCalled();
  rerender(<DevicePicker devices={[]} value="" disabled={false} visible onChange={change}/>);fireEvent.click(trigger);expect(screen.getByText("暂无设备")).toBeTruthy();fireEvent.keyDown(trigger,{key:"Enter"});expect(change).not.toHaveBeenCalled();
 });
});
