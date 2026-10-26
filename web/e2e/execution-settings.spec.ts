import { expect, test, type WebSocketRoute } from "@playwright/test";
import type { Thread } from "../src/generated/protocol";
const token=`s_${"d".repeat(64)}`;
const first:Thread={threadId:"settings-one",title:"模型设置会话",cwd:"/work/ariel",updatedAt:"2026-10-26T00:00:00Z",runtime:"inProgress",turns:[{turnId:"active",status:"inProgress",items:[{itemId:"reply",role:"assistant",text:"现有内容持续展示"}]}],pendingInteractions:[]};
const second:Thread={...first,threadId:"settings-two",title:"另一个会话",runtime:"idle",turns:[]};
const models=[{id:"model-a",name:"Model A",description:"A model with a long descriptive name to verify wrapping",efforts:[{id:"low",description:"Faster reasoning"},{id:"high",description:"Deeper reasoning"}],defaultEffort:"low",tiers:[{id:"native-priority",name:"Fast",description:"Increased usage"}],defaultTier:null,inputModalities:["text","image"],isDefault:true},{id:"model-b",name:"Model B",description:"Another model",efforts:[{id:"medium",description:"Balanced"}],defaultEffort:"medium",tiers:[],defaultTier:null,inputModalities:["text"],isDefault:false}];
test("execution settings preserve the transcript, fence old events and remain usable on mobile and desktop",async({page})=>{
 let socket:WebSocketRoute;const methods:string[]=[];let saveRequest:any=null;let watchCount=0;
 const selections:Record<string,any>={"settings-one":{model:"model-a",effort:"high",speed:{mode:"tier",tierId:"native-priority"}},"settings-two":{model:"model-b",effort:"medium",speed:{mode:"inherit"}}};
 const snapshot=(id:string)=>({revision:`revision-${id}`,freshness:"fresh",writable:true,values:selections[id]});
 await page.addInitScript(t=>sessionStorage.setItem("ariel.web-session.v1",t),token);
 await page.routeWebSocket("**/ws",route=>{socket=route;route.onMessage(raw=>{
  const m=JSON.parse(String(raw));if(m.type==="hello"){route.send(JSON.stringify({type:"hello.ok",v:1,connectionId:"settings",relayEpoch:"relay",sessionToken:token}));return}if(m.type!=="request")return;methods.push(m.method);let data:Record<string,unknown>={};
  if(m.method==="device.list")data={devices:[{deviceId:"mac",deviceName:"Settings Mac",agentOnline:true,codexReady:true,agentEpoch:"epoch",capabilities:{autoLoad:true,send:true,queue:true}}]};
  if(m.method==="thread.list")data={threads:[first,second]};
  if(m.method==="thread.cache.watch")data={watchId:`cache-${m.params.threadId}`,freshness:"fresh",agentOnline:true,agentEpoch:"epoch",cacheStreamId:`cache-stream-${m.params.threadId}`,seq:1,capturedAt:first.updatedAt,thread:m.params.threadId===first.threadId?first:second};
  if(m.method==="model.catalog.read")data={catalog:{models},settingsSupported:true,agentEpoch:"epoch"};
  if(m.method==="thread.settings.watch"){watchCount++;data={subscriptionId:`settings-${watchCount}`,streamId:`stream-${watchCount}`,seq:1,agentEpoch:"epoch",settings:snapshot(m.params.threadId)}}
  if(m.method==="thread.settings.update"){saveRequest=m;return}
  route.send(JSON.stringify({type:"response",v:1,requestId:m.requestId,outcome:"accepted",data}));
 })});
 await page.goto("/");await page.getByRole("button",{name:"展开项目 ariel，/work/ariel"}).click();await page.getByText(first.title,{exact:true}).click();
 const trigger=page.getByRole("button",{name:"模型与推理设置"});await expect(trigger).toContainText("Model A");await page.getByRole("textbox",{name:"发送消息"}).fill("输入草稿保留");await trigger.click();
 const dialog=page.getByRole("dialog",{name:"模型与推理设置"});await expect(dialog).toBeVisible();const box=await dialog.boundingBox();expect(box!.x).toBeGreaterThanOrEqual(0);expect(box!.x+box!.width).toBeLessThanOrEqual(390);expect(await dialog.evaluate(e=>e.scrollWidth<=e.clientWidth)).toBe(true);
 await expect(dialog).toHaveScreenshot("mobile-execution-settings.png",{animations:"disabled",caret:"hide"});
 await page.getByLabel("推理强度",{exact:true}).selectOption("low");await page.getByRole("button",{name:"保存设置"}).click();await expect.poll(()=>saveRequest).not.toBeNull();await expect(page.getByRole("button",{name:"加入队列"})).toBeDisabled();await expect(page.getByText("现有内容持续展示")).toBeVisible();await expect(page.getByRole("textbox",{name:"发送消息"})).toHaveValue("输入草稿保留");
 selections[first.threadId]=saveRequest.params.patch;socket!.send(JSON.stringify({type:"response",v:1,requestId:saveRequest.requestId,outcome:"accepted",data:{agentEpoch:"epoch",settings:snapshot(first.threadId)}}));await expect(dialog).toBeHidden();await expect(trigger).toContainText("低");
 await page.getByRole("button",{name:/打开会话列表/}).click();await page.getByText(second.title,{exact:true}).click();await expect(trigger).toContainText("Model B");
 socket!.send(JSON.stringify({type:"event",v:1,event:"thread.settings.update",deviceId:"mac",agentEpoch:"epoch",threadId:first.threadId,subscriptionId:"settings-1",streamId:"stream-1",seq:99,settings:snapshot(first.threadId)}));await expect(trigger).toContainText("Model B");
 expect(methods.filter(m=>m==="model.catalog.read")).toHaveLength(1);expect(methods.filter(m=>m==="thread.subscribe")).toHaveLength(0);expect(methods.filter(m=>m==="thread.history")).toHaveLength(0);
 await page.setViewportSize({width:1280,height:820});await trigger.click();await expect(dialog).toBeVisible();const desktop=await dialog.boundingBox();expect(desktop!.width).toBeLessThanOrEqual(440);expect(desktop!.x).toBeGreaterThan(0);await page.keyboard.press("Escape");await expect(dialog).toBeHidden();await expect(trigger).toBeFocused();
});
