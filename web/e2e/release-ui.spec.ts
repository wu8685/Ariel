import {expect,test,type Page,type WebSocketRoute} from "@playwright/test";
import type {Thread} from "../src/generated/protocol";
const token=`s_${"e".repeat(64)}`;
const first:Thread={threadId:"first",title:"停止后重新编辑",cwd:"/tmp/project",updatedAt:"2026-10-27T00:00:00Z",runtime:"idle",permissions:{sandbox:"read_only",approval:"never"},turns:[{turnId:"seed",status:"completed",items:[{itemId:"seed-a",role:"assistant",text:"前序内容保留"}]},{turnId:"stopped",status:"interrupted",items:[{itemId:"user",role:"user",text:"需要重新编辑的原文"}]}],pendingInteractions:[],queuedMessages:[]};
const second:Thread={...first,threadId:"second",title:"另一个隔离草稿",turns:[]};
const models=[{id:"m",name:"Model",description:"",efforts:[],defaultEffort:"",tiers:[],defaultTier:null,inputModalities:["text"],isDefault:true}];
async function fixture(page:Page,unknown=false){
 const edits:any[]=[];let route:WebSocketRoute;let seq=1;let current=structuredClone({...first,historyComplete:false,recentComplete:true});
 const push=(thread:Thread)=>{current=thread;route.send(JSON.stringify({type:"event",v:1,event:"thread.cache.update",deviceId:"mac",threadId:"first",watchId:"watch-first",agentEpoch:"epoch",agentOnline:true,freshness:"fresh",cacheStreamId:"cache-stream",seq:++seq,capturedAt:"2026-10-27T00:00:01Z",thread}));};
 await page.addInitScript(t=>sessionStorage.setItem("ariel.web-session.v1",t),token);
 await page.routeWebSocket("**/ws",s=>{route=s;s.onMessage(raw=>{
  const m=JSON.parse(String(raw));if(m.type==="hello"){s.send(JSON.stringify({type:"hello.ok",v:1,connectionId:"c",relayEpoch:"r",sessionToken:token}));return;}if(m.type!=="request")return;
  let data:any={};if(m.method==="device.list")data={devices:[{deviceId:"mac",deviceName:"Fixture Mac",agentEpoch:"epoch",agentOnline:true,codexReady:true,capabilities:{autoLoad:true,threadBootstrap:true}},{deviceId:"other",deviceName:"Other Mac",agentEpoch:"epoch-other",agentOnline:true,codexReady:true,capabilities:{autoLoad:true,threadBootstrap:true}}]};
  if(m.method==="thread.list")data={threads:[current,second].map(thread=>({...thread,...(m.params.searchTerm?{searchSnippet:"这是隔离搜索摘要，用较长的内容确认紧凑列表仍然允许两行摘要自然增高，且不会裁掉标题或日期，保留完整的导航点击区域。"}:{})})),nextCursor:""};
  if(m.method==="thread.history")data={turns:[current.turns[0],{turnId:"paged-old",status:"completed",items:[{itemId:"paged-user",role:"user",text:"分页锚点旧内容"}]}],nextCursor:""};
  if(m.method==="thread.cache.watch")data={watchId:`watch-${m.params.threadId}`,freshness:"fresh",agentOnline:true,agentEpoch:"epoch",cacheStreamId:"cache-stream",seq,capturedAt:"2026-10-27T00:00:00Z",thread:m.params.threadId==="first"?current:second};
  if(m.method==="model.catalog.read")data={catalog:{models},settingsSupported:true,editSupported:true,agentEpoch:"epoch"};
  if(m.method==="thread.settings.watch")data={subscriptionId:"settings",streamId:"settings-stream",seq:1,agentEpoch:"epoch",settings:{revision:"revision",freshness:"fresh",writable:true,values:{model:"m",effort:"",speed:{mode:"inherit"}}}};
  if(m.method==="thread.subscribe")data={subscriptionId:`sub-${m.params.threadId}`};
  if(m.method==="turn.edit.read")data={target:{turnId:"stopped",itemId:"user",text:"需要重新编辑的原文",fingerprint:"a".repeat(64)},agentEpoch:"epoch"};
  if(m.method==="turn.edit"){edits.push(m);if(unknown){s.send(JSON.stringify({type:"response",v:1,requestId:m.requestId,outcome:"unknown",error:{code:"OUTCOME_UNKNOWN",message:"fixture receipt lost"}}));return;}current={...current,turns:[current.turns[0],{turnId:"replacement",status:"completed",items:[{itemId:"new-user",role:"user",text:m.params.text},{itemId:"new-assistant",role:"assistant",text:"新回复"}]}]};data={turnId:"replacement",replacedTurnId:"stopped",operationId:m.params.operationId,agentEpoch:"epoch"};}
  s.send(JSON.stringify({type:"response",v:1,requestId:m.requestId,outcome:"accepted",data}));
  if(m.method==="thread.subscribe"){const thread=m.params.threadId==="first"?current:second;s.send(JSON.stringify({type:"event",v:1,event:"thread.snapshot",deviceId:"mac",threadId:thread.threadId,subscriptionId:`sub-${thread.threadId}`,streamId:`stream-${seq}`,seq:1,thread}));}
  if(m.method==="turn.edit")push(current);
 });});
 await page.goto("/");await page.getByRole("button",{name:/展开项目 project/}).click();
 return {edits,push};
}
for(const width of [320,390,1280]){
 test(`compact global controls and themed sidebar at ${width}`,async({page})=>{
  await page.setViewportSize({width,height:844});await fixture(page);
  const sidebar=page.getByLabel("会话列表",{exact:true});const toolbar=sidebar.locator(".sidebar-toolbar");const picker=page.getByRole("combobox",{name:"设备"});
  const rects=await Promise.all([toolbar.boundingBox(),picker.boundingBox(),sidebar.locator(".project-name").first().boundingBox(),sidebar.locator(".thread-row .thread-title").first().boundingBox()]);
  expect(rects.every(Boolean)).toBe(true);expect(rects[1]!.y-rects[0]!.y-rects[0]!.height).toBeGreaterThanOrEqual(8);expect(rects[3]!.x-rects[2]!.x).toBeGreaterThanOrEqual(12);
  await picker.click();const popup=page.getByRole("listbox",{name:"设备列表"});await expect(popup).toBeVisible();const box=await popup.boundingBox();expect(box!.x+box!.width).toBeLessThanOrEqual(width);await picker.press("Escape");await expect(popup).toHaveCount(0);
  const circles=await sidebar.locator("button.circle-control").evaluateAll(buttons=>buttons.map(b=>{const r=b.getBoundingClientRect(),v=b.querySelector(".circle-visual")!.getBoundingClientRect(),svg=b.querySelector("svg")!.getBoundingClientRect();return {x:r.x,right:r.right,y:r.y,width:r.width,height:r.height,visual:v.width,icon:svg.width};}));
  expect(circles.length).toBeGreaterThanOrEqual(5);for(const r of circles){expect(r.width).toBe(44);expect(r.height).toBe(44);expect(r.visual).toBe(28);expect(r.icon).toBe(14);expect(r.x).toBeGreaterThanOrEqual(0);expect(r.right).toBeLessThanOrEqual(width);}
  for(let i=1;i<5;i++)expect(circles[i].x).toBeGreaterThanOrEqual(circles[i-1].right);
  const colors=await sidebar.evaluate(n=>({project:getComputedStyle(n.querySelector(".project-toggle")!).backgroundColor,children:getComputedStyle(n.querySelector(".project-threads")!).backgroundColor,guideWidth:getComputedStyle(n.querySelector(".project-threads")!).borderLeftWidth,selection:getComputedStyle(n.querySelector(".thread-row")!).userSelect}));expect(colors.project).not.toBe(colors.children);expect(colors.guideWidth).toBe("0px");expect(colors.selection).toBe("none");
  await expect(sidebar.locator(".project-toggle")).toHaveCSS("height","44px");
  for(const row of await sidebar.locator(".thread-row").all()){
   await expect(row).toHaveCSS("height","44px");
   const [box,title,date]=await Promise.all([row.boundingBox(),row.locator(".thread-title").boundingBox(),row.locator(".thread-date").boundingBox()]);
   expect(title!.y).toBeGreaterThanOrEqual(box!.y);expect(date!.y).toBeGreaterThanOrEqual(title!.y+title!.height);expect(date!.y+date!.height).toBeLessThanOrEqual(box!.y+box!.height);
  }
  await expect(page).toHaveScreenshot(`compact-sidebar-${width}.png`);
  await page.getByText(first.title,{exact:true}).click();await expect(page.getByRole("button",{name:"编辑并重新发送"})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBe(width);
 });
 test(`compact sidebar search summaries keep natural height at ${width}`,async({page})=>{
  await page.setViewportSize({width,height:844});await fixture(page);await page.getByRole("searchbox",{name:"搜索会话"}).fill("隔离");
  await expect(page.locator(".thread-snippet")).toHaveCount(2);
  for(const row of await page.locator(".thread-row").all()){
   const [box,title,snippet,date]=await Promise.all([row.boundingBox(),row.locator(".thread-title").boundingBox(),row.locator(".thread-snippet").boundingBox(),row.locator(".thread-date").boundingBox()]);
   // Two summary lines add their actual height, plus 6px padding, 2px border and two 2px gaps.
   expect(box!.height).toBeGreaterThan(44);expect(box!.height).toBeCloseTo(title!.height+snippet!.height+date!.height+12,1);expect(snippet!.height).toBeLessThanOrEqual(29);expect(title!.y).toBeGreaterThanOrEqual(box!.y);expect(snippet!.y).toBeGreaterThanOrEqual(title!.y+title!.height);expect(date!.y).toBeGreaterThanOrEqual(snippet!.y+snippet!.height);expect(date!.y+date!.height).toBeLessThanOrEqual(box!.y+box!.height);expect(box!.x+box!.width).toBeLessThanOrEqual(width);
   await expect(row.locator(".thread-title")).toHaveCSS("font-size","13px");
  }
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBe(width);
  await page.getByRole("button",{name:"清空搜索",exact:true}).click();await expect(page.locator(".thread-snippet")).toHaveCount(0);
  await page.getByText(first.title,{exact:true}).click();await expect(page.getByText("前序内容保留",{exact:true})).toBeVisible();
 });
 test(`stopped edit replaces native-shaped history and preserves drafts at ${width}`,async({page})=>{
  await page.setViewportSize({width,height:844});const {edits}=await fixture(page);await page.getByText(first.title,{exact:true}).click();
  const composer=page.getByRole("textbox",{name:"发送消息"});const pencil=page.getByRole("button",{name:"编辑并重新发送"}); await expect(pencil).toBeVisible();const messageBox=await page.locator(".message.user .message-body").boundingBox(),pencilBox=await pencil.boundingBox();expect(pencilBox!.y).toBeGreaterThanOrEqual(messageBox!.y+messageBox!.height);
  await composer.fill("现有草稿");await pencil.click();await expect(composer).toHaveValue("现有草稿");expect(edits).toHaveLength(0);await composer.fill("");
  await pencil.click();await expect(composer).toHaveValue("需要重新编辑的原文");await composer.fill("替换后的用户文本");
  await page.getByRole("button",{name:/打开会话列表/}).click();await page.getByText(second.title,{exact:true}).click();await expect(composer).toHaveValue("");
  await page.getByRole("button",{name:/打开会话列表/}).click();await page.getByText(first.title,{exact:true}).click();await expect(composer).toHaveValue("替换后的用户文本");
  await page.getByRole("button",{name:"重新发送",exact:true}).click();await expect(composer).toHaveValue("");await expect(page.locator('.message.user')).toHaveText("替换后的用户文本");await expect(page.getByText("新回复",{exact:true})).toBeVisible();await expect(page.getByText("前序内容保留",{exact:true})).toBeVisible();expect(edits).toHaveLength(1);expect(edits[0].params).toMatchObject({expectedTurnId:"stopped",expectedItemId:"user",agentEpoch:"epoch",text:"替换后的用户文本"});expect(edits[0].params).not.toHaveProperty("patch");await expect(page.getByRole("button",{name:"编辑并重新发送"})).toHaveCount(0);
 });
}

test("unknown edit preserves text and cannot replay; external continuation invalidates the target",async({page})=>{
 const {edits,push}=await fixture(page,true);await page.getByText(first.title,{exact:true}).click();await page.getByRole("button",{name:"编辑并重新发送"}).click();const input=page.getByRole("textbox",{name:"发送消息"});await input.fill("未知结果的草稿");await page.getByRole("button",{name:"重新发送",exact:true}).click();await expect(page.getByText(/编辑结果不确定/)).toBeVisible();await expect(input).toHaveValue("未知结果的草稿");await expect(page.getByRole("button",{name:"重新发送",exact:true})).toBeDisabled();push(first);await expect(input).toHaveValue("未知结果的草稿");expect(edits).toHaveLength(1);
 await page.getByRole("button",{name:"取消编辑",exact:true}).click();await input.fill("");await page.getByRole("button",{name:"编辑并重新发送"}).click();await input.fill("另一个客户端续聊时保留的草稿");push({...first,runtime:"inProgress",turns:[first.turns[1],{turnId:"external",status:"inProgress",items:[{itemId:"external-user",role:"user",text:"外部续聊"}]}]});await expect(input).toHaveValue("另一个客户端续聊时保留的草稿");await expect(page.getByRole("button",{name:"重新发送",exact:true})).toBeDisabled();expect(edits).toHaveLength(1);
});

test("device picker stays inside a keyboard-sized viewport",async({page})=>{
 await fixture(page);await page.setViewportSize({width:390,height:160});await page.getByRole("combobox",{name:"设备"}).click();const list=page.getByRole("listbox",{name:"设备列表"});await expect(list).toBeVisible();const box=await list.boundingBox();expect(box!.y).toBeGreaterThanOrEqual(8);expect(box!.y+box!.height).toBeLessThanOrEqual(160);await page.getByRole("combobox",{name:"设备"}).press("Escape");await expect(page.getByLabel("会话列表",{exact:true})).toHaveClass(/open/);
});

test("two watchers replace the same stopped turn without restoring it from cached pagination",async({page,context})=>{
 const other=await context.newPage();const a=await fixture(page),b=await fixture(other);for(const p of [page,other]){await p.getByText(first.title,{exact:true}).click();await expect(p.locator('[data-item-id="user"]')).toContainText("需要重新编辑的原文");if(!await p.getByText("分页锚点旧内容",{exact:true}).count())await p.getByRole("button",{name:"加载更早消息",exact:true}).click();await expect(p.getByText("分页锚点旧内容",{exact:true})).toBeVisible();}
 const replacement:Thread={...first,turns:[first.turns[0],{turnId:"replacement",status:"completed",items:[{itemId:"replaced-user",role:"user",text:"跨客户端的新原文"}]}]};a.push(replacement);b.push(replacement);for(const p of [page,other]){await expect(p.locator('.message.user')).toHaveText("跨客户端的新原文");await expect(p.getByText("需要重新编辑的原文",{exact:true})).toHaveCount(0);await expect(p.getByText("分页锚点旧内容",{exact:true})).toHaveCount(0);await expect(p.getByText("前序内容保留",{exact:true})).toBeVisible();}await other.close();
});
