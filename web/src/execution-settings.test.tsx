// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {StrictMode} from "react";
import { ExecutionSettingsControl } from "./execution-settings";
import { ExecutionSettingsStore, changeModel, type ExecutionValues } from "./model-settings";
import type { ArielSocket } from "./client";
afterEach(cleanup);
const models=[{id:"m",name:"Model",description:"Current",efforts:[{id:"low",description:"Low"},{id:"high",description:"High"}],defaultEffort:"low",tiers:[{id:"native-priority",name:"Fast",description:"increased usage"}],defaultTier:null,inputModalities:["text","image"],isDefault:true},{id:"text",name:"Text",description:"Text only",efforts:[{id:"low",description:"Low"}],defaultEffort:"low",tiers:[],defaultTier:null,inputModalities:["text"],isDefault:false}];
const values:ExecutionValues={model:"m",effort:"high",speed:{mode:"tier",tierId:"native-priority"}};
function fixture(){
 const listeners=new Set<(e:any)=>void>();
 const request=vi.fn(async(method:string,_device:string,params:any)=>({type:"response",v:1,requestId:"r",outcome:"accepted",data:method==="model.catalog.read"?{catalog:{models},settingsSupported:true,agentEpoch:"epoch"}:method==="thread.settings.watch"?{subscriptionId:"settings-1",streamId:"stream",seq:1,agentEpoch:"epoch",settings:{revision:"r1",values,freshness:"fresh",writable:true}}:method==="thread.settings.update"?{agentEpoch:"epoch",settings:{revision:"r2",values:params.patch,freshness:"fresh",writable:true}}:{}}));
 const client={request,sessionGeneration:1,subscribeEvents:(f:any)=>{listeners.add(f);return()=>listeners.delete(f)}} as unknown as ArielSocket;
 return {client,request,listeners};
}
describe("conversation execution settings",()=>{
	it("keeps confirmed settings visible but read-only while disconnected",async()=>{
		const f=fixture(),props={client:f.client,store:new ExecutionSettingsStore(),deviceId:"mac",agentEpoch:"epoch",targetKey:"mac:t",threadId:"t",ownerReady:true,queued:false,busy:false,hasImages:false,onState:()=>{}};
		const result=render(<ExecutionSettingsControl {...props} connected/>);await waitFor(()=>expect(screen.getByRole("button",{name:"模型与推理设置"}).textContent).toContain("Model"));
		result.rerender(<ExecutionSettingsControl {...props} connected={false}/>);expect(screen.getByRole("button",{name:"模型与推理设置"}).textContent).toContain("Model");
	});
	it("keeps legacy Agent controls hidden through StrictMode's shared pending read",async()=>{
		const f=fixture(),original=f.request.getMockImplementation()!;f.request.mockImplementation(async(method,device,params)=>method==="model.catalog.read"?{type:"response",v:1,requestId:"r",outcome:"accepted",data:{}} as any:original(method,device,params));
		render(<StrictMode><ExecutionSettingsControl client={f.client} store={new ExecutionSettingsStore()} deviceId="mac" agentEpoch="epoch" targetKey="mac:t" threadId="t" connected ownerReady queued={false} busy={false} hasImages={false} onState={()=>{}}/></StrictMode>);
		await act(async()=>{await new Promise(r=>setTimeout(r,10))});expect(screen.queryByRole("button",{name:"模型与推理设置"})).toBeNull();
	});
	it("does not silently rebase an open edit onto another client's settings",async()=>{
		const f=fixture();render(<ExecutionSettingsControl client={f.client} store={new ExecutionSettingsStore()} deviceId="mac" agentEpoch="epoch" targetKey="mac:t" threadId="t" connected ownerReady queued={false} busy={false} hasImages={false} onState={()=>{}}/>);
		await waitFor(()=>expect(screen.getByRole("button",{name:"模型与推理设置"}).textContent).toContain("Model"));fireEvent.click(screen.getByRole("button",{name:"模型与推理设置"}));
		await act(async()=>{for(const fn of f.listeners)fn({type:"event",event:"thread.settings.update",deviceId:"mac",agentEpoch:"epoch",threadId:"t",subscriptionId:"settings-1",streamId:"stream",seq:2,settings:{revision:"other-client",values:{...values,speed:{mode:"standard"}},freshness:"fresh",writable:true}})});
		fireEvent.click(screen.getByRole("button",{name:"保存设置"}));await waitFor(()=>expect(f.request).toHaveBeenCalledWith("thread.settings.update","mac",expect.objectContaining({expectedRevision:"r1"})));
	});
	it("releases an accepted watch with an unverifiable baseline",async()=>{
		const f=fixture(),original=f.request.getMockImplementation()!;
		f.request.mockImplementation(async(method,device,params)=>method==="thread.settings.watch"?{type:"response",v:1,requestId:"r",outcome:"accepted",data:{subscriptionId:"settings-bad",streamId:"s",settings:{}}} as any:original(method,device,params));
		render(<ExecutionSettingsControl client={f.client} store={new ExecutionSettingsStore()} deviceId="mac" agentEpoch="epoch" targetKey="mac:t" threadId="t" connected ownerReady queued={false} busy={false} hasImages={false} onState={()=>{}}/>);
		await waitFor(()=>expect(f.request).toHaveBeenCalledWith("thread.settings.unwatch","mac",{subscriptionId:"settings-bad"}));
	});
 it("keeps valid choices and explicitly resets unsupported combinations",()=>{
  const result=changeModel(values,models[1]);expect(result.changedDefaults).toBe(true);expect(result.values).toEqual({model:"text",effort:"low",speed:{mode:"inherit"}});
 });
 it("uses native catalog choices, saves next-turn settings and accepts settings-only events",async()=>{
  const f=fixture(),onState=vi.fn();render(<ExecutionSettingsControl client={f.client} store={new ExecutionSettingsStore()} deviceId="mac" agentEpoch="epoch" targetKey="mac:t" threadId="t" connected ownerReady queued={false} busy={false} hasImages={false} onState={onState}/>);
  await waitFor(()=>expect(screen.getByRole("button",{name:"模型与推理设置"}).textContent).toContain("Model"));
  fireEvent.click(screen.getByRole("button",{name:"模型与推理设置"}));
  expect((screen.getByLabelText("速度") as HTMLSelectElement).value).toBe("tier:native-priority");
  fireEvent.change(screen.getByLabelText("推理强度"),{target:{value:"low"}});fireEvent.click(screen.getByRole("button",{name:"保存设置"}));
  await waitFor(()=>expect(f.request).toHaveBeenCalledWith("thread.settings.update","mac",expect.objectContaining({threadId:"t",expectedRevision:"r1",patch:expect.objectContaining({effort:"low"})})));
  await waitFor(()=>expect(screen.queryByRole("dialog")).toBeNull());
  await act(async()=>{for(const fn of f.listeners)fn({type:"event",event:"thread.settings.update",deviceId:"mac",agentEpoch:"epoch",threadId:"t",subscriptionId:"settings-1",streamId:"stream",seq:2,settings:{revision:"r3",values:{...values,effort:"high"},freshness:"fresh",writable:true}})});
  expect(screen.getByRole("button",{name:"模型与推理设置"}).textContent).toContain("高");
 });
 it("draft choices are independent and text-only models block image submission",async()=>{
  const f=fixture(),store=new ExecutionSettingsStore(),onState=vi.fn();render(<ExecutionSettingsControl client={f.client} store={store} deviceId="mac" agentEpoch="epoch" targetKey="mac:draft" draft connected ownerReady queued={false} busy={false} hasImages onState={onState}/>);
  await waitFor(()=>expect(screen.getByRole("button",{name:"模型与推理设置"})).toBeTruthy());fireEvent.click(screen.getByRole("button",{name:"模型与推理设置"}));fireEvent.change(screen.getByLabelText("模型"),{target:{value:"text"}});fireEvent.click(screen.getByRole("button",{name:"保存设置"}));
  await waitFor(()=>expect(onState).toHaveBeenLastCalledWith(expect.objectContaining({key:"mac:draft",imagesBlocked:true,draftSettings:expect.objectContaining({model:"text"})})));
  expect(f.request.mock.calls.some(c=>c[0]==="thread.settings.update")).toBe(false);expect(store.drafts.get("another")).toBeUndefined();
 });
});
