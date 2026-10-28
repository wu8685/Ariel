// @vitest-environment jsdom
import {act,cleanup,renderHook} from "@testing-library/react";
import {afterEach,expect,it,vi} from "vitest";
import {useWorkspaceLayout} from "./workspace-layout";

afterEach(()=>{cleanup();vi.unstubAllGlobals()});
function viewport(initial:boolean){
  const listeners=new Set<(e:MediaQueryListEvent)=>void>();
  const media={matches:initial,addEventListener:(_name:string,fn:(e:MediaQueryListEvent)=>void)=>listeners.add(fn),removeEventListener:(_name:string,fn:(e:MediaQueryListEvent)=>void)=>listeners.delete(fn)};
  const matchMedia=vi.fn(()=>media);vi.stubGlobal("matchMedia",matchMedia);
  return {listeners,matchMedia,resize(wide:boolean){act(()=>{media.matches=wide;for(const notify of listeners)notify({matches:wide} as MediaQueryListEvent)})}};
}
it("docks the wide sidebar by default and never closes it on thread selection",()=>{
  const media=viewport(true),{result}=renderHook(()=>useWorkspaceLayout());
  expect(media.matchMedia).toHaveBeenCalledWith("(min-width: 1200px)");expect(result.current.wide).toBe(true);expect(result.current.sidebarVisible).toBe(true);
  act(()=>result.current.afterThreadSelection());expect(result.current.sidebarVisible).toBe(true);
  act(()=>result.current.setSidebarOpen(false));expect(result.current.sidebarVisible).toBe(false);
  act(()=>result.current.afterThreadSelection());expect(result.current.sidebarVisible).toBe(false);
});
it("keeps drawer and dock preferences independent across repeated breakpoint changes",()=>{
  const media=viewport(false),{result}=renderHook(()=>useWorkspaceLayout());
  expect(result.current.sidebarVisible).toBe(true);act(()=>result.current.afterThreadSelection());expect(result.current.sidebarVisible).toBe(false);
  media.resize(true);expect(result.current.sidebarVisible).toBe(true);
  act(()=>result.current.setSidebarOpen(false));media.resize(false);expect(result.current.sidebarVisible).toBe(false);
  act(()=>result.current.setSidebarOpen(true));media.resize(true);expect(result.current.sidebarVisible).toBe(false);
  act(()=>result.current.setSidebarOpen(true));media.resize(false);expect(result.current.sidebarVisible).toBe(false);
  media.resize(true);expect(result.current.sidebarVisible).toBe(true);
});
it("cleans up media listeners and starts with a fresh dock preference on remount",()=>{
  const media=viewport(true),first=renderHook(()=>useWorkspaceLayout());expect(media.listeners.size).toBe(1);
  act(()=>first.result.current.setSidebarOpen(false));first.unmount();expect(media.listeners.size).toBe(0);
  const next=renderHook(()=>useWorkspaceLayout());expect(next.result.current.sidebarVisible).toBe(true);
});
