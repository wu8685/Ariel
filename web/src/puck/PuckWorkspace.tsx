import { useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { avatarColors, avatarShapes, deriveAvatar, PuckAvatar, type AvatarDesign, type AvatarShape, type PuckState } from "./PuckAvatar";
import "./puck.css";

type PuckResponse = { outcome: string; data?: Record<string, unknown>; error?: { code?: string; message?: string } };
type Request = (method: string, target: string, params: Record<string, unknown>) => Promise<PuckResponse>;
type SubscribeEvents = (listener: (event: Record<string, unknown>) => void) => () => void;
type Device = { deviceId: string; name?: string; capabilities?: Record<string, boolean> };
type Assistant = { assistantId: string; name: string; avatar?: string; description?: string; brainDeviceId: string; status: string; unreadCount?: number; lastMessagePreview?: string };
type Message = { id: string; role: string; kind: string; text: string; cardJSON?: string; createdAt?: string; workSummary?: string; reaction?: string };
const reactionChoices = ["👍", "❤️", "😂", "🎉", "👀"] as const;

function responseError(response: PuckResponse): string {
  return response.error?.message || response.error?.code || "请求失败，请稍后重试。";
}
function arrayData<T>(value: unknown): T[] { return Array.isArray(value) ? value as T[] : []; }
function normalizeMessage(value: Record<string, unknown>): Message {
  return {
    id: String(value.id ?? value.ID ?? crypto.randomUUID()), role: String(value.role ?? value.Role ?? "assistant"),
    kind: String(value.kind ?? value.Kind ?? "text"), text: String(value.text ?? value.Text ?? ""),
    cardJSON: String(value.cardJSON ?? value.CardJSON ?? ""), createdAt: String(value.createdAt ?? value.CreatedAt ?? ""), workSummary: String(value.workSummary ?? value.WorkSummary ?? ""), reaction: String(value.reaction ?? value.Reaction ?? ""),
  };
}
function workData(message: Message): Record<string, unknown> { try { return message.workSummary ? JSON.parse(message.workSummary) as Record<string, unknown> : {}; } catch { return {}; } }
function workLabel(message: Message, now: number): string {
	const data = workData(message); const elapsed = Number(data.elapsedMs);
	if (Number.isFinite(elapsed)) return `工作了 ${Math.max(0, Math.ceil(elapsed / 1000))} 秒 · 执行 ${Math.max(0, Number(data.steps) || 0)} 步`;
	const started = new Date(String(data.startedAt || "")).getTime();
	return Number.isFinite(started) ? `工作了 ${Math.max(0, Math.floor((now - started) / 1000))} 秒` : "";
}
function reactions(message: Message): Record<string,string> { try { return message.reaction ? JSON.parse(message.reaction) as Record<string,string> : {}; } catch { return {}; } }
function mergeHistory(current: Message[], durable: Message[]): Message[] {
  if (durable.length === 0) return current;
  const durableIDs = new Set(durable.map(message => message.id));
  const pending = current.filter(message => {
    if (durableIDs.has(message.id)) return false;
    if (message.kind === "processing") {
      const base = message.id.replace(/-processing$/, "");
      const user = durable.find(item => item.id === base);
      const answered = durable.some(item => item.id === `${base}-ack` || (item.role === "assistant" && user?.createdAt && item.createdAt && item.createdAt >= user.createdAt));
      return !answered;
    }
    return message.role === "user" || message.kind === "error";
  });
  return [...durable, ...pending];
}
function avatarDesign(assistant: Assistant): AvatarDesign {
  try { const value = JSON.parse(assistant.avatar || "") as AvatarDesign; if (value.shape && value.color) return value; } catch { /* deterministic fallback */ }
  return deriveAvatar(assistant.assistantId);
}
function moveRadio(event: KeyboardEvent<HTMLButtonElement>, values: readonly string[], current: string, select: (value: string) => void) {
	let index = values.indexOf(current);
	if (["ArrowRight", "ArrowDown"].includes(event.key)) index = (index + 1) % values.length;
	else if (["ArrowLeft", "ArrowUp"].includes(event.key)) index = (index - 1 + values.length) % values.length;
	else if (event.key === "Home") index = 0;
	else if (event.key === "End") index = values.length - 1;
	else return;
	event.preventDefault();
	const next = values[index];
	const group = event.currentTarget.parentElement;
	select(next);
	window.requestAnimationFrame(() => group?.querySelector<HTMLButtonElement>(`[data-avatar-choice="${next}"]`)?.focus());
}
function messageState(messages: Message[]): PuckState {
  const latest = [...messages].reverse();
  const attention = latest.find(message => ["question", "authorization"].includes(message.kind) && String(cardData(message).cardState || "pending") === "pending");
  if (attention?.kind === "question") return "waiting_user";
  if (attention?.kind === "authorization") return "awaiting_authorization";
  const task = latest.find(message => message.kind === "task");
  const status = String(task ? cardData(task).status || "" : "");
  if (["running", "dispatching"].includes(status) || latest.some(message => ["processing", "greeting_processing"].includes(message.kind))) return "working";
  if (status === "failed" || latest[0]?.kind === "error") return "error";
  if (["done", "completed"].includes(status)) return "done";
  return "idle";
}
function cardData(message: Message): Record<string, unknown> {
  try { return message.cardJSON ? JSON.parse(message.cardJSON) as Record<string, unknown> : {}; } catch { return {}; }
}
function expiryLabel(value: unknown): string {
  if (!value) return "未说明";
  const milliseconds = new Date(String(value)).getTime() - Date.now();
  if (!Number.isFinite(milliseconds)) return "未说明";
  const minutes = Math.max(0, Math.ceil(milliseconds / 60000));
  return milliseconds <= 0 ? "已过期" : `${minutes} 分钟后过期`;
}
function decisionLabel(data: Record<string, unknown>): string {
  const rejected = String(data.cardState) === "rejected" || String(data.decision) === "reject";
  const decidedAt = data.decidedAt ? new Date(String(data.decidedAt)).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", timeZone: "Asia/Shanghai" }) : "";
  return `${rejected ? "已拒绝" : "已允许一次"}${decidedAt ? ` · ${decidedAt}` : ""}`;
}
const taskLabels: Record<string, string> = { running:"进行中", dispatching:"派发中", waiting_user:"等待回复", awaiting_authorization:"等待授权", stopped:"已停止", reconciling:"正在核对", done:"已完成", failed:"失败" };
function Card({ message, submitting, onAction }: { message: Message; submitting: boolean; onAction: (kind:string,data:Record<string,unknown>,value:string)=>void }) {
  const data = cardData(message);
  const terminal = String(data.cardState || "pending") !== "pending";
  if (message.kind === "question") return <section className={`puck-card puck-question-card${terminal ? " terminal" : ""}`} role="group" aria-label="问题卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">?</span><strong>{terminal ? "已回答" : "需要你决定"}</strong></div>
    <p>{message.text}</p>
    {!terminal && <div className="puck-card-actions puck-options">{arrayData<string>(data.options).map(option => <button className="puck-chip" key={option} type="button" disabled={submitting} onClick={()=>onAction("question",data,option)}>{option}</button>)}</div>}
    {data.answer != null && <p className="puck-card-result"><span aria-hidden="true">✓</span> 你选择了 <strong>{String(data.answer)}</strong></p>}
  </section>;
  const expiresAt = data.expiresAt ? new Date(String(data.expiresAt)).getTime() : Number.POSITIVE_INFINITY;
  const expired = message.kind === "authorization" && Number.isFinite(expiresAt) && expiresAt <= Date.now();
  if (message.kind === "authorization" && expired && !terminal) return <section className="puck-card puck-authorization-card terminal compact" role="group" aria-label="授权卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">◇</span><strong>授权已过期</strong><span className="puck-card-result">已过期</span></div>
  </section>;
  if (message.kind === "authorization") return <section className={`puck-card puck-authorization-card${terminal ? " terminal" : ""}`} role="group" aria-label="授权卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">◇</span><strong>{terminal ? "授权已处理" : "需要授权"}</strong></div>
    <p>{message.text}</p>
    <dl><dt>动作</dt><dd>{String(data.action || "未说明")}</dd><dt>主机</dt><dd>{String(data.deviceId || "未说明")}</dd><dt>目录</dt><dd className="puck-mono">{String(data.cwd || "未说明")}</dd><dt>有效期</dt><dd title={data.expiresAt ? new Date(String(data.expiresAt)).toLocaleString("zh-CN") : undefined}>{expiryLabel(data.expiresAt)}</dd></dl>
    {(data.risk != null || data.riskTags != null) && <p className="puck-risk"><span aria-hidden="true">△</span>风险：{String(data.risk || arrayData<string>(data.riskTags).join("、") || "无额外风险")}</p>}
    {!terminal ? <div className="puck-card-actions"><button className="puck-btn" type="button" disabled={submitting} onClick={()=>onAction("authorization",data,"reject")}>拒绝</button><button className="puck-btn primary" type="button" disabled={submitting} onClick={()=>onAction("authorization",data,"approve_once")}>仅这一次允许</button></div> : <p className="puck-card-result">{decisionLabel(data)}</p>}
  </section>;
  if (message.kind === "task") {
    const status = String(data.status || "pending");
    return <section className="puck-card puck-task-card" role="group" aria-label="任务卡片">
      <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">›_</span><strong>{message.text}</strong><span className="puck-state" data-state={status}><i aria-hidden="true"/>{taskLabels[status] || status || "待处理"}</span></div>
      <details><summary><span aria-hidden="true">›</span>{message.workSummary || "查看工作过程"}</summary><p>{String(data.summary || "执行细节按需展开，不在对话中刷屏。")}</p></details>
      {Boolean(data.deviceId && data.threadId) && <div className="puck-card-actions"><button className="puck-btn" type="button" onClick={()=>onAction("task",data,"open")}>打开会话 <span aria-hidden="true">↗</span></button></div>}
    </section>;
  }
  return null;
}

export function PuckWorkspace({ request, subscribeEvents, state, reason, devices, onBack, onOpenThread }: { request: Request; subscribeEvents?: SubscribeEvents; state: "ready" | "unavailable" | "disabled"; reason?: string; devices: Device[]; onBack?: () => void; onOpenThread?: (deviceId: string, threadId: string) => void }) {
  const [assistants, setAssistants] = useState<Assistant[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [brainDeviceID, setBrainDeviceID] = useState("");
	const [submittingCard, setSubmittingCard] = useState("__none__");
	const [mobilePanel, setMobilePanel] = useState<"" | "assistants" | "info">("");
	const avatarUpdateSequence = useRef(0);
  const selectedRef = useRef("");
  const [clock, setClock] = useState(() => Date.now());
  const [reactionTarget, setReactionTarget] = useState("");
  const [editingDescription, setEditingDescription] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState("");
  const faviconOriginal = useRef<{ href:string; type:string } | undefined>(undefined);
  const readyBrains = useMemo(() => devices.filter(device => device.capabilities?.puckBrainReady), [devices]);
  const selected = assistants.find(assistant => assistant.assistantId === selectedID);
  const puckState = messageState(messages);
  const busy = messages.some(message => ["processing", "greeting_processing"].includes(message.kind) || (message.kind === "task" && ["running", "dispatching", "waiting_user", "awaiting_authorization"].includes(String(cardData(message).status))));
  selectedRef.current = selectedID;

  useEffect(() => {
	if (!messages.some(message => workData(message).state === "running")) return;
	setClock(Date.now()); const timer = window.setInterval(() => setClock(Date.now()), 1000);
	return () => window.clearInterval(timer);
  }, [messages]);

  async function loadList() {
    const response = await request("puck.list", "relay", {});
    if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
    const next = arrayData<Assistant>(response.data?.assistants);
    setAssistants(next.map(item => item.assistantId === selectedRef.current ? { ...item, unreadCount: 0 } : item)); setSelectedID(current => current && next.some(item => item.assistantId === current) ? current : next[0]?.assistantId || "");
  }
  useEffect(() => { if (state !== "ready") return; void loadList(); const timer = window.setInterval(() => void loadList(), 5000); return () => window.clearInterval(timer); }, [state]);
  useEffect(() => {
    if (!selectedID) { setMessages([]); return; }
	setEditingDescription(false); setDescriptionDraft(assistants.find(item => item.assistantId === selectedID)?.description || "");
	setAssistants(current => current.map(item => item.assistantId === selectedID ? { ...item, unreadCount: 0 } : item));
	void request("puck.read", "relay", { assistantId: selectedID });
    let active = true;
    void request("puck.history", "relay", { assistantId: selectedID }).then(response => {
      if (active && response.outcome === "accepted") {
        const durable = arrayData<Record<string, unknown>>(response.data?.messages).map(normalizeMessage);
        setMessages(current => mergeHistory(current, durable));
      }
    });
    return () => { active = false; };
  }, [selectedID]);
	useEffect(() => {
		if (!selectedID) return;
		let active = true;
		let watchID = "";
		const unsubscribe = subscribeEvents?.(event => {
			if (!active || event.event !== "puck.event" || event.assistantId !== selectedID || event.kind !== "history") return;
			const payload = event.payload as Record<string, unknown> | undefined;
			const durable = arrayData<Record<string, unknown>>(payload?.messages).map(normalizeMessage);
			setMessages(current => mergeHistory(current, durable));
			void request("puck.read", "relay", { assistantId: selectedID });
		});
		if (subscribeEvents) {
			void request("puck.watch", "relay", { assistantId: selectedID }).then(response => {
				const id = response.outcome === "accepted" ? String(response.data?.watchId || "") : "";
				if (!id) return;
				if (!active) void request("puck.unwatch", "relay", { watchId: id });
				else watchID = id;
			});
		}
		const timer = window.setInterval(() => { void request("puck.history", "relay", { assistantId: selectedID }).then(response => {
			if (response.outcome !== "accepted") return;
			const durable = arrayData<Record<string, unknown>>(response.data?.messages).map(normalizeMessage);
			setMessages(current => mergeHistory(current, durable));
		}); }, subscribeEvents ? 10000 : 2000);
		return () => {
			active = false;
			unsubscribe?.();
			window.clearInterval(timer);
			if (watchID) void request("puck.unwatch", "relay", { watchId: watchID });
		};
	}, [selectedID, subscribeEvents]);

  async function createAssistant() {
    const deviceID = brainDeviceID || readyBrains[0]?.deviceId;
    if (!name.trim() || !deviceID) { setNotice("请填写名称并选择可用的大脑主机。"); return; }
    const response = await request("puck.create", "relay", { name: name.trim(), brainDeviceId: deviceID, allowedScope: {} });
    if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
    const assistant = response.data?.assistant as Assistant;
    setAssistants(current => [...current, assistant]); setSelectedID(assistant.assistantId); setName(""); setCreating(false);
    const greeting = response.data?.greeting as Record<string,unknown> | undefined;
	setMessages(greeting?.state === "processing" ? [{ id:String(greeting.messageId), role:"assistant", kind:"processing", text:"正在准备…", workSummary:JSON.stringify({startedAt:greeting.startedAt,state:"running"}) }] : []);
  }
  async function send() {
    const text = draft.trim(); if (!selected || !text) return;
    if (["停", "停止"].includes(text)) { setDraft(""); await stop(); return; }
    const clientMessageId = crypto.randomUUID(); setDraft(""); const localStartedAt = new Date().toISOString();
    setMessages(current => [...current, { id: clientMessageId, role: "user", kind: "text", text }, { id: `${clientMessageId}-processing`, role: "assistant", kind: "processing", text: "正在处理…", workSummary:JSON.stringify({startedAt:localStartedAt,state:"running"}) }]);
    const response = await request("puck.message.send", "relay", { assistantId: selected.assistantId, clientMessageId, text });
    if (response.outcome !== "accepted") setMessages(current => current.map(item => item.id === `${clientMessageId}-processing` ? { ...item, kind: "error", text: responseError(response), workSummary:"" } : item));
	else setMessages(current => current.map(item => item.id === `${clientMessageId}-processing` ? { ...item, workSummary:JSON.stringify({startedAt:response.data?.startedAt || localStartedAt,state:"running"}) } : item));
  }
  async function stop() {
    if (!selected) return;
    const response = await request("puck.stop", "relay", { assistantId: selected.assistantId });
    if (response.outcome === "accepted" && response.data?.message) setMessages(current => [...current.filter(item => item.kind !== "processing"), normalizeMessage(response.data!.message as Record<string, unknown>)]);
    else if (response.outcome !== "accepted") setMessages(current => [...current, { id: crypto.randomUUID(), role: "assistant", kind: "error", text: responseError(response) }]);
  }
	async function respondCard(kind:string,data:Record<string,unknown>,value:string) {
		if (!selected) return;
		if (kind === "task") {
			onOpenThread?.(String(data.deviceId || ""), String(data.threadId || ""));
			return;
		}
		const identity = String(data.authorizationId || data.questionId || "");
		if (submittingCard === identity) return;
		setSubmittingCard(identity);
		const response = kind === "authorization"
			? await request("puck.authorization.decide","relay",{assistantId:selected.assistantId,authorizationId:String(data.authorizationId||""),decision:value})
			: await request("puck.question.answer","relay",{assistantId:selected.assistantId,questionId:String(data.questionId||""),answer:value});
		if (response.outcome === "accepted") setMessages(current => current.map(message => {
			const card = cardData(message);
			if (String(card.authorizationId || card.questionId || "") !== identity) return message;
			return {...message, cardJSON: JSON.stringify({...card, cardState: kind === "question" ? "answered" : value === "reject" ? "rejected" : "consumed", ...(kind === "question" ? {answer:value} : {})})};
		})); else setNotice(responseError(response));
		setSubmittingCard("__none__");
	}
	async function updateAvatar(shape: AvatarShape, color: string) {
		if (!selected) return;
		const currentDesign = avatarDesign(selected);
		if (currentDesign.shape === shape && currentDesign.color === color) return;
		const previousAvatar = selected.avatar;
		const optimistic = { ...selected, avatar: JSON.stringify({ shape, color }) };
		const sequence = ++avatarUpdateSequence.current;
		setAssistants(current => current.map(item => item.assistantId === selected.assistantId ? optimistic : item));
		const response = await request("puck.update", "relay", { assistantId: selected.assistantId, avatar: { shape, color } });
		if (sequence !== avatarUpdateSequence.current) return;
		if (response.outcome !== "accepted") {
			setAssistants(current => current.map(item => item.assistantId === selected.assistantId ? { ...item, avatar: previousAvatar } : item));
			setNotice(responseError(response));
			return;
		}
		const confirmed = response.data?.assistant as Assistant | undefined;
		if (confirmed) setAssistants(current => current.map(item => item.assistantId === confirmed.assistantId ? confirmed : item));
	}
	async function saveDescription() {
		if (!selected) return; const description = descriptionDraft.trim();
		const response = await request("puck.update", "relay", { assistantId:selected.assistantId, description });
		if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
		const confirmed = response.data?.assistant as Assistant | undefined;
		setAssistants(current => current.map(item => item.assistantId === selected.assistantId ? (confirmed || { ...item, description }) : item)); setEditingDescription(false);
	}
	async function reactTo(message: Message, emoji: string) {
		if (!selected) return; const response = await request("puck.message.react", "relay", { assistantId:selected.assistantId, messageId:message.id, emoji });
		if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
		setMessages(current => current.map(item => item.id === message.id ? { ...item, reaction:String(response.data?.reaction || "") } : item)); setReactionTarget("");
	}

	useEffect(() => {
		const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]'); if (!link) return;
		if (!faviconOriginal.current) faviconOriginal.current = { href:link.getAttribute("href") || "", type:link.getAttribute("type") || "" };
		return () => { const original=faviconOriginal.current; if (original) { link.href=original.href; link.type=original.type; } };
	}, []);
	useEffect(() => {
		if (!selected || navigator.userAgent.includes("jsdom")) return;
		let cancelled=false; const svg=document.querySelector<SVGSVGElement>(".puck-conversation > header .puck-avatar svg"); if (!svg) return;
		const clone=svg.cloneNode(true) as SVGSVGElement; clone.setAttribute("xmlns","http://www.w3.org/2000/svg"); clone.setAttribute("width","128"); clone.setAttribute("height","128");
		clone.querySelectorAll<SVGElement>(".puck-avatar-body,.puck-avatar-ears,.puck-avatar-stem").forEach(node=>node.setAttribute("fill",avatarDesign(selected).color)); clone.querySelectorAll<SVGElement>(".puck-avatar-face").forEach(node=>node.setAttribute("fill","#0b0b0c"));
		const image=new Image(); image.onload=()=>{ if(cancelled)return; const canvas=document.createElement("canvas"); canvas.width=canvas.height=128; const context=canvas.getContext("2d"); if(!context)return; context.drawImage(image,0,0,128,128); const link=document.querySelector<HTMLLinkElement>('link[rel="icon"]'); if(link){link.type="image/png";link.href=canvas.toDataURL("image/png");} };
		image.src=`data:image/svg+xml;charset=utf-8,${encodeURIComponent(new XMLSerializer().serializeToString(clone))}`; return()=>{cancelled=true;};
	}, [selectedID, selected?.avatar]);

  if (state !== "ready") return <main className="puck-unavailable">
    <button className="puck-btn" type="button" onClick={onBack}>返回会话</button>
    <div><span className="puck-mark">P</span><h1>Puck 不可用</h1><p>{reason === "PUCK_REQUIRES_PASSKEY_HTTPS" ? "Puck 仅支持 Passkey + HTTPS" : "管理员尚未开启 Puck。"}</p><small>现有 Codex 会话不受影响。</small></div>
  </main>;

  const statusLabel = puckState === "waiting_user" ? "等你回复" : puckState === "awaiting_authorization" ? "等待授权" : puckState === "working" || puckState === "thinking" ? "思考中" : puckState === "error" ? "出错" : "可用";
  return <main className="puck-workspace">
    {mobilePanel && <button className="puck-panel-backdrop" aria-label="关闭面板" onClick={()=>setMobilePanel("")}/>}
    <aside className={`puck-assistants ${mobilePanel === "assistants" ? "mobile-open" : ""}`}>
      <header><b>Puck</b><button className="puck-icon-btn puck-add" type="button" onClick={() => setCreating(true)} aria-label="新建助理">＋</button></header>
      <div className="puck-assistant-list">{assistants.map(item => <button className={`puck-assistant-row${item.assistantId === selectedID ? " active" : ""}`} key={item.assistantId} onClick={() => { setSelectedID(item.assistantId); setMobilePanel(""); }}>
        <PuckAvatar assistantId={item.assistantId} name={item.name} design={avatarDesign(item)} state={item.assistantId === selectedID ? puckState : "idle"} size="small" attention={Boolean(item.unreadCount) || item.assistantId === selectedID && ["waiting_user","awaiting_authorization","error"].includes(puckState)}/>
        <span className="puck-assistant-copy"><strong>{item.name}</strong><small>{item.assistantId === selectedID && messages.at(-1)?.text || item.lastMessagePreview || (item.status === "ready" ? "在线" : "准备中")}</small></span>
        {Boolean(item.unreadCount) && <span className="puck-unread-count" aria-label={`${item.name}有 ${item.unreadCount} 条未读消息`}>{Math.min(99,item.unreadCount || 0)}</span>}
      </button>)}</div>
      <button className="puck-back" type="button" onClick={onBack}><span aria-hidden="true">←</span> Codex 会话</button>
    </aside>

    <section className="puck-conversation">
      <header>
        <button className="puck-mobile-menu puck-icon-btn" type="button" aria-label="打开助理列表" onClick={()=>setMobilePanel("assistants")}>☰</button>
        {selected && <PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState}/>}
        <div className="puck-conversation-title"><strong>{selected?.name || "Puck"}</strong><small>{selected ? "你的个人助理" : "创建一个助理开始"}</small></div>
        {selected && <div className="puck-header-actions"><button className="puck-mobile-menu puck-icon-btn" type="button" aria-label="查看助理信息" onClick={()=>setMobilePanel("info")}>ⓘ</button>{busy && <button className="puck-stop puck-icon-btn" type="button" aria-label="停止全部工作" onClick={() => void stop()}><span aria-hidden="true"/></button>}</div>}
      </header>
      <div className="puck-transcript" aria-live="polite">{assistants.length === 0 ? <div className="puck-empty">
        <PuckAvatar assistantId="puck-empty" name="Puck" size="large" animated={false}/><h2>创建你的第一个助理</h2><p>给它一个名字，选择在哪台设备上思考。</p><button className="puck-btn primary puck-btn-md" type="button" aria-label="新建助理" onClick={() => setCreating(true)}>新建助理</button>
      </div> : messages.map((message,index) => {
        const continued = index > 0 && messages[index - 1].role === message.role;
        const groupEnd = index === messages.length - 1 || messages[index + 1].role !== message.role;
        return <article key={message.id} className={`puck-message ${message.role} ${message.kind}${continued ? " continued" : ""}`}>
          {message.role === "assistant" && !continued && selected && <PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState} size="small"/>}
          <div className="puck-message-content">
            <Card message={message} submitting={submittingCard === String(cardData(message).authorizationId || cardData(message).questionId || "")} onAction={(kind,data,value)=>void respondCard(kind,data,value)}/>
            {!["question", "authorization", "task"].includes(message.kind) && <div className="puck-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{message.text}</ReactMarkdown>{["processing", "greeting_processing", "acknowledged"].includes(message.kind) && <span className="puck-thinking-dots" aria-hidden="true"><i/><i/><i/></span>}</div>}
            {workLabel(message,clock) && <div className="puck-work-summary">{workLabel(message,clock)}</div>}
            <div className="puck-reactions">{Object.entries(reactions(message)).map(([actor,emoji]) => <button type="button" className="puck-reaction-pill" aria-label={`${actor === "user" ? "你" : "助理"}回应 ${emoji}，1 个`} key={actor} onClick={()=>actor === "user" && void reactTo(message,emoji)}>{emoji} <span>1</span></button>)}<button type="button" className="puck-reaction-add" aria-label="给消息添加回应" onClick={()=>setReactionTarget(current=>current === message.id ? "" : message.id)}>＋</button>{reactionTarget === message.id && <div className="puck-reaction-picker">{reactionChoices.map(emoji=><button type="button" key={emoji} aria-label={`回应 ${emoji}`} onClick={()=>void reactTo(message,emoji)}>{emoji}</button>)}</div>}</div>
            {groupEnd && message.createdAt && <time>{new Date(message.createdAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", timeZone: "Asia/Shanghai" })}</time>}
          </div>
        </article>;
      })}</div>
      {notice && <div className="puck-notice" role="alert">{notice}<button className="puck-btn" onClick={() => setNotice("")}>关闭</button></div>}
      {selected && <footer><div className="puck-composer"><textarea rows={1} aria-label="给 Puck 发消息" value={draft} onChange={event => setDraft(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void send(); } }} placeholder="说说你想做什么…"/><button className="primary puck-send puck-icon-btn" type="button" aria-label="发送" onClick={() => void send()} disabled={!draft.trim()}><span aria-hidden="true">↑</span></button></div></footer>}
    </section>

    <aside className={`puck-info ${mobilePanel === "info" ? "mobile-open" : ""}`}>
      <header><h3>助理信息</h3></header>
      {selected ? <div className="puck-info-content">
        <div className="puck-avatar-editor"><PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState} size="large"/><strong>{selected.name}</strong>{editingDescription ? <div className="puck-description-editor"><textarea aria-label="助理说明" maxLength={512} value={descriptionDraft} onChange={event=>setDescriptionDraft(event.target.value)}/><div><button className="puck-btn" type="button" onClick={()=>{setEditingDescription(false);setDescriptionDraft(selected.description || "");}}>取消</button><button className="puck-btn primary" type="button" aria-label="保存说明" onClick={()=>void saveDescription()}>保存</button></div></div> : <><p>{selected.description || "尚未添加说明"}</p><button className="puck-description-edit" type="button" aria-label="编辑说明" onClick={()=>{setDescriptionDraft(selected.description || "");setEditingDescription(true);}}>编辑说明</button></>}</div>
        <div className="puck-avatar-picker"><div className="puck-picker-section"><span className="puck-avatar-picker-label">形状</span><div className="puck-shape-options" role="radiogroup" aria-label="头像形状">{avatarShapes.map(option => { const checked = avatarDesign(selected).shape === option.value; return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-shape-option ${checked ? "selected" : ""}`} key={option.value} onClick={()=>void updateAvatar(option.value,avatarDesign(selected).color)} onKeyDown={event=>moveRadio(event,avatarShapes.map(item=>item.value),avatarDesign(selected).shape,value=>void updateAvatar(value as AvatarShape,avatarDesign(selected).color))}><PuckAvatar assistantId={`${selected.assistantId}-${option.value}`} name={option.label} design={{shape:option.value,color:avatarDesign(selected).color}} size="small" animated={false}/></button>; })}</div></div>
          <div className="puck-picker-section"><span className="puck-avatar-picker-label">颜色</span><div className="puck-color-options" role="radiogroup" aria-label="头像颜色">{avatarColors.map(option => { const checked = avatarDesign(selected).color === option.value; return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-color-option ${checked ? "selected" : ""}`} style={{"--puck-choice-color":option.value} as CSSProperties} key={option.value} onClick={()=>void updateAvatar(avatarDesign(selected).shape,option.value)} onKeyDown={event=>moveRadio(event,avatarColors.map(item=>item.value),avatarDesign(selected).color,value=>void updateAvatar(avatarDesign(selected).shape,value))}><span aria-hidden="true"/></button>; })}</div></div>
        </div>
        <section className="puck-info-details"><h4>详情</h4><dl><dt>名称</dt><dd>{selected.name}</dd><dt>状态</dt><dd><span className="puck-info-status" data-state={puckState}><i aria-hidden="true"/>{statusLabel}</span></dd><dt>大脑主机</dt><dd><span aria-hidden="true">▣</span> {devices.find(device => device.deviceId === selected.brainDeviceId)?.name || selected.brainDeviceId}</dd></dl></section>
      </div> : <p className="puck-info-empty">选择助理后查看设置。</p>}
    </aside>

    {creating && <div className="puck-dialog-backdrop" role="presentation"><form className="puck-dialog" onSubmit={event => { event.preventDefault(); void createAssistant(); }}><h2>新建助理</h2><label>助理名称<input aria-label="助理名称" value={name} onChange={event => setName(event.target.value)} autoFocus maxLength={80}/></label><label>大脑主机<select aria-label="大脑主机" value={brainDeviceID} onChange={event => setBrainDeviceID(event.target.value)}><option value="">请选择</option>{readyBrains.map(device => <option key={device.deviceId} value={device.deviceId}>{device.name || device.deviceId}</option>)}</select></label><div><button className="puck-btn puck-btn-md" type="button" onClick={() => setCreating(false)}>取消</button><button className="puck-btn puck-btn-md primary" type="submit">创建</button></div></form></div>}
  </main>;
}
