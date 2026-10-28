import {expect,test} from "@playwright/test";
import type {Thread} from "../src/generated/protocol";

const token=`s_${"a".repeat(64)}`;
const thread:Thread={threadId:"async-fixture",title:"异步问题验收",cwd:"/tmp/async-fixture",updatedAt:"2026-10-28T00:00:00Z",runtime:"inProgress",pendingInteractions:[],turns:[{turnId:"turn",status:"inProgress",items:[{itemId:"message",role:"assistant",text:"我会继续处理独立的工作。"}]}]};
const question={id:"question",fingerprint:"fingerprint",turnId:"turn",itemId:"message",index:0,title:"请选择后续验证方式",options:["先完成测试，再发布","暂不发布，只保留本地修改"],status:"open"};
for(const width of [320,390,1280])test(`native async questions are visible and nonblocking at ${width}px`,async({page})=>{
  await page.setViewportSize({width,height:900});
  let socket:{send(data:string):void}|undefined;
  const requests:Record<string,any>[]=[];
  await page.addInitScript(token=>sessionStorage.setItem("ariel.web-session.v1",token),token);
  await page.routeWebSocket("**/ws",ws=>{socket=ws;ws.onMessage(raw=>{
    const m=JSON.parse(String(raw));if(m.type==="hello"){ws.send(JSON.stringify({type:"hello.ok",v:1,connectionId:"async",relayEpoch:"relay",sessionToken:token}));return;}
    if(m.type!=="request")return;requests.push(m);
    let data:Record<string,unknown>={};
    if(m.method==="device.list")data={asyncQuestionsVersion:1,devices:[{deviceId:"mac",deviceName:"隔离 Mac",agentOnline:true,codexReady:true,agentEpoch:"epoch",capabilities:{autoLoad:true,queue:true,send:true}}]};
    if(m.method==="thread.list")data={threads:[thread],nextCursor:""};
    if(m.method==="thread.cache.watch")data={watchId:"watch",freshness:"fresh",agentOnline:true,agentEpoch:"epoch",cacheStreamId:"cache",seq:1,capturedAt:thread.updatedAt,thread,asyncQuestions:{version:1,writable:false,questions:[question],warnings:[]}};
    ws.send(JSON.stringify({type:"response",v:1,requestId:m.requestId,outcome:"accepted",data}));
  });});
  await page.goto("/");
  await page.getByRole("button",{name:"展开项目 async-fixture，/tmp/async-fixture"}).click();
  await page.getByText(thread.title,{exact:true}).click();
  await expect(page.getByRole("heading",{name:question.title})).toBeVisible();
  expect(requests.find(m=>m.method==="thread.cache.watch")?.params).toEqual({threadId:thread.threadId,asyncQuestionsVersion:1});
  await expect(page.getByRole("radio",{name:question.options[0]})).toBeDisabled();
  await expect(page.getByText(/尚未通过异步回答兼容性验证/)).toBeVisible();
  await page.getByLabel("发送消息").fill("主草稿不应被问题卡片阻塞");
  const card=page.getByRole("region",{name:"Codex 异步问题"});
  const box=await card.boundingBox();expect(box).not.toBeNull();expect(box!.x).toBeGreaterThanOrEqual(0);expect(box!.x+box!.width).toBeLessThanOrEqual(width);
  await expect(card).toHaveScreenshot(`async-question-${width}.png`,{animations:"disabled",caret:"hide"});
  await page.getByRole("button",{name:"跳过",exact:true}).click();
  await expect(page.getByText("已在本页跳过问题")).toBeVisible();
  await page.getByRole("button",{name:"重新展开问题"}).click();
  const completed={...thread,runtime:"idle",turns:[{...thread.turns[0],status:"completed"}]};
  socket!.send(JSON.stringify({type:"event",v:1,event:"thread.cache.update",deviceId:"mac",threadId:thread.threadId,watchId:"watch",freshness:"fresh",agentOnline:true,agentEpoch:"epoch",cacheStreamId:"cache",seq:2,capturedAt:thread.updatedAt,thread:completed,asyncQuestions:{version:1,writable:false,questions:[{...question,status:"answered",answer:"先完成测试，再发布"}],warnings:[]}}));
  await expect(card.getByText("已回答",{exact:true})).toBeVisible();
  await expect(card.getByText("先完成测试，再发布",{exact:true})).toBeVisible();
  await expect(page.getByLabel("发送消息")).toHaveValue("主草稿不应被问题卡片阻塞");
  expect(requests.filter(m=>m.method==="thread.subscribe"||m.method==="interaction.respond"||m.method==="thread.questions.respond"||m.method==="turn.start"||m.method.startsWith("queue."))).toEqual([]);
});
