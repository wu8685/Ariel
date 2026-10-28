import {createContext,useContext,useState} from "react";
import "./async-questions.css";

export type AsyncQuestion={id:string;fingerprint:string;turnId:string;itemId:string;index:number;title:string;options:string[];status:"open"|"answered"|"ended"|"unverified";answer?:string};
export type QuestionSnapshot={version:1;writable:boolean;questions:AsyncQuestion[];warnings:{turnId:string;itemId:string;code:string}[]};
const text=(x:unknown,max:number):x is string=>typeof x==="string"&&Array.from(x).length<=max;
const object=(x:unknown):x is Record<string,unknown>=>typeof x==="object"&&x!==null&&!Array.isArray(x);
export function parseQuestionSnapshot(raw:unknown):QuestionSnapshot|null {
  if(!object(raw)||raw.version!==1||typeof raw.writable!=="boolean"||!Array.isArray(raw.questions)||!Array.isArray(raw.warnings)||raw.questions.length>64||raw.warnings.length>64)return null;
  if(new TextEncoder().encode(JSON.stringify(raw)).length>65536)return null;
  const ids=new Set<string>();
  for(const q of raw.questions){
    if(!object(q)||!text(q.id,256)||!q.id||ids.has(q.id)||!text(q.fingerprint,128)||!q.fingerprint||!text(q.turnId,256)||!q.turnId||!text(q.itemId,256)||!q.itemId||!Number.isInteger(q.index)||Number(q.index)<0||Number(q.index)>=16||!text(q.title,2048)||!q.title.trim()||!Array.isArray(q.options)||q.options.length>32||q.options.some(o=>!text(o,512)||!o.trim())||!["open","answered","ended","unverified"].includes(String(q.status))||q.answer!==undefined&&!text(q.answer,8192)||(q.status==="answered")!==(typeof q.answer==="string"&&!!q.answer.trim()))return null;
    ids.add(q.id);
  }
  for(const w of raw.warnings){if(!object(w)||!text(w.turnId,256)||!text(w.itemId,256)||!text(w.code,64)||!w.code)return null;}
  return raw as QuestionSnapshot;
}

type Draft={value:string;custom:boolean;skipped:boolean};
// All question text and unsubmitted answers are memory-only. No storage,
// durable history, checkpoints, socket, polling or follower lives here.
export class QuestionMemory {
  private drafts=new Map<string,Draft>();
  private pages=new Map<string,{snapshot:QuestionSnapshot;bytes:number}>();
  private bytes=0;
  private invalidScopes=new Set<string>();
  private markInvalid(scope:string){this.invalidScopes.delete(scope);this.invalidScopes.add(scope);while(this.invalidScopes.size>512)this.invalidScopes.delete(this.invalidScopes.values().next().value!);}
  get(key:string):Draft{return this.drafts.get(key)||{value:"",custom:false,skipped:false};}
  set(key:string,draft:Draft){this.drafts.delete(key);this.drafts.set(key,draft);while(this.drafts.size>1024)this.drafts.delete(this.drafts.keys().next().value!);}
  receive(scope:string,raw:unknown,turns:{turnId:string;items:{itemId:string}[]}[],mergeItems=false):boolean{
    const snapshot=parseQuestionSnapshot(raw);if(!snapshot){this.markInvalid(scope);return false;}
    if(snapshot.questions.some(q=>!turns.some(t=>t.turnId===q.turnId&&t.items.some(i=>i.itemId===q.itemId)))){this.markInvalid(scope);return false;}
    this.invalidScopes.delete(scope);
    for(const turn of turns){
      const key=`${scope}\0${turn.turnId}`;
      const previous=this.pages.get(key);
      const fresh=snapshot.questions.filter(q=>q.turnId===turn.turnId).map(q=>{const confirmed=mergeItems&&q.status==="unverified"?previous?.snapshot.questions.find(old=>old.id===q.id&&old.fingerprint===q.fingerprint&&old.status==="answered"):undefined;return confirmed||q;});
      const questions=mergeItems?[...(previous?.snapshot.questions||[]).filter(q=>!turn.items.some(i=>i.itemId===q.itemId)),...fresh]:fresh;
      const page:QuestionSnapshot={version:1,writable:snapshot.writable,questions,warnings:snapshot.warnings.filter(w=>!w.turnId||w.turnId===turn.turnId)};
      const bytes=new TextEncoder().encode(JSON.stringify(page)).length;
      this.bytes-=previous?.bytes||0;this.pages.delete(key);this.pages.set(key,{snapshot:page,bytes});this.bytes+=bytes;
    }
    while(this.bytes>8*1024*1024||this.pages.size>512){const key=this.pages.keys().next().value!;this.bytes-=this.pages.get(key)!.bytes;this.pages.delete(key);}
    return true;
  }
  forTurn(scope:string,turnId:string):QuestionSnapshot|undefined{return this.pages.get(`${scope}\0${turnId}`)?.snapshot;}
  invalid(scope:string){return this.invalidScopes.has(scope);}
  clear(){this.drafts.clear();this.pages.clear();this.invalidScopes.clear();this.bytes=0;}
}

type Props={questions:AsyncQuestion[];writable:boolean;memory:QuestionMemory;scope:string;disabledReason:string;onRespond?:(question:AsyncQuestion,answer:string)=>void;};
export const AsyncQuestionContext=createContext<{memory:QuestionMemory;scope:string;disabledReason:string}|null>(null);
export function TurnQuestionWarnings({turnId}:{turnId:string}){
  const context=useContext(AsyncQuestionContext);if(!context)return null;
  const snapshot=context.memory.forTurn(context.scope,turnId);
  return <>{snapshot?.warnings.filter(w=>!w.itemId).map((w,index)=><p className="async-question-note" role="status" key={index}>本页异步问题暂不能安全展示，会话仍可使用；请在 Codex 查看（{w.code}）。</p>)}</>;
}
export function TurnQuestionCards({turnId,itemId}:{turnId:string;itemId:string}){
  const context=useContext(AsyncQuestionContext);if(!context)return null;
  const snapshot=context.memory.forTurn(context.scope,turnId);if(!snapshot)return null;
  return <><AsyncQuestionCards memory={context.memory} scope={context.scope} disabledReason={context.disabledReason} questions={snapshot.questions.filter(q=>q.itemId===itemId)} writable={false}/>{snapshot.warnings.filter(w=>w.itemId===itemId).map((warning,index)=><p className="async-question-note" role="status" key={index}>这条消息的问题暂不能安全展示，请在 Codex 中查看（{warning.code}）。</p>)}</>;
}
export function AsyncQuestionCards(props:Props){return <>{props.questions.map(q=><QuestionCard {...props} question={q} key={`${props.scope}\0${q.id}\0${q.fingerprint}`}/>)}</>;}
function QuestionCard({question:q,writable,memory,scope,disabledReason,onRespond}:Props&{question:AsyncQuestion}){
  const key=`${scope}\0${q.id}\0${q.fingerprint}`;
  const [draft,setDraft]=useState(()=>memory.get(key));
  const update=(patch:Partial<Draft>)=>{const next={...draft,...patch};memory.set(key,next);setDraft(next);};
  const reason=q.status==="unverified"?"此页仅包含部分历史，回答状态请在 Codex 中查看。":q.status==="ended"?"已结束，未回答":q.status==="answered"?"":disabledReason||(!writable?"当前 Desktop 尚未通过异步回答兼容性验证，请在 Codex 中回答。":"");
  const disabled=q.status!=="open"||!!reason;
  if(draft.skipped&&q.status==="open")return <section className="async-question skipped"><span>已在本页跳过问题</span><button type="button" onClick={()=>update({skipped:false})}>重新展开问题</button></section>;
  return <section className="async-question" aria-label="Codex 异步问题" data-question-id={q.id}>
    <header><span>Codex 有一个问题</span><small>{q.status==="answered"?"已回答":q.status==="unverified"?"部分历史":q.status==="ended"?"已结束":"可继续执行"}</small></header>
    <h4>{q.title}</h4>
    {q.status==="answered"?<p className="async-question-answer">{q.answer}</p>:<>
      {q.options.length>0&&<fieldset disabled={disabled}><legend className="sr-only">选择回答</legend>{q.options.map((option,index)=><label className="async-question-option" key={index}><input type="radio" name={key} aria-label={option} checked={!draft.custom&&draft.value===option} onChange={()=>update({value:option,custom:false})}/><span>{option}</span></label>)}{writable&&<label className="async-question-option"><input type="radio" name={key} aria-label="其他" checked={draft.custom} onChange={()=>update({custom:true,value:q.options.includes(draft.value)?"":draft.value})}/><span>其他</span></label>}</fieldset>}
      {writable&&(q.options.length===0||draft.custom)&&<textarea rows={2} maxLength={8192} aria-label={`回答：${q.title}`} value={draft.value} disabled={disabled} placeholder="输入回答…" onChange={e=>update({value:e.target.value})}/>}
      {reason&&<p className="async-question-note" role="status">{reason}</p>}
      {q.status==="open"&&<footer><button type="button" onClick={()=>update({skipped:true})}>跳过</button>{writable&&<button type="button" className="primary" disabled={disabled||!draft.value.trim()||!onRespond} onClick={()=>onRespond?.(q,draft.value.trim())}>发送回答</button>}</footer>}
    </>}
  </section>;
}
