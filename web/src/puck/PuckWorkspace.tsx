import { useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { avatarColors, avatarShapes, deriveAvatar, PuckAvatar, type AvatarDesign, type AvatarShape, type PuckState } from "./PuckAvatar";
import "./puck.css";

type PuckResponse = { outcome: string; data?: Record<string, unknown>; error?: { code?: string; message?: string } };
type Request = (method: string, target: string, params: Record<string, unknown>) => Promise<PuckResponse>;
type SubscribeEvents = (listener: (event: Record<string, unknown>) => void) => () => void;
type Device = { deviceId: string; name?: string; capabilities?: Record<string, boolean> };
type Assistant = { assistantId: string; name: string; avatar?: string; description?: string; brainDeviceId: string; status: string };
type Message = { id: string; role: string; kind: string; text: string; cardJSON?: string; createdAt?: string; workSummary?: string };

function responseError(response: PuckResponse): string {
  return response.error?.message || response.error?.code || "请求失败，请稍后重试。";
}
function arrayData<T>(value: unknown): T[] { return Array.isArray(value) ? value as T[] : []; }
function normalizeMessage(value: Record<string, unknown>): Message {
  return {
    id: String(value.id ?? value.ID ?? crypto.randomUUID()), role: String(value.role ?? value.Role ?? "assistant"),
    kind: String(value.kind ?? value.Kind ?? "text"), text: String(value.text ?? value.Text ?? ""),
    cardJSON: String(value.cardJSON ?? value.CardJSON ?? ""), createdAt: String(value.createdAt ?? value.CreatedAt ?? ""), workSummary: String(value.workSummary ?? value.WorkSummary ?? ""),
  };
}
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
  if (["running", "dispatching"].includes(status) || latest.some(message => message.kind === "processing")) return "working";
  if (status === "failed" || latest[0]?.kind === "error") return "error";
  if (["done", "completed"].includes(status)) return "done";
  return "idle";
}
function cardData(message: Message): Record<string, unknown> {
  try { return message.cardJSON ? JSON.parse(message.cardJSON) as Record<string, unknown> : {}; } catch { return {}; }
}
function Card({ message, submitting, onAction }: { message: Message; submitting: boolean; onAction: (kind:string,data:Record<string,unknown>,value:string)=>void }) {
  const data = cardData(message);
  const terminal = String(data.cardState || "pending") !== "pending";
  if (message.kind === "question") return <section className="puck-card puck-question-card" role="group" aria-label="问题卡片"><strong>{terminal ? "已回答" : "需要你决定"}</strong><p>{message.text}</p><div className="puck-options">{arrayData<string>(data.options).map(option => <button key={option} type="button" disabled={terminal || submitting} onClick={()=>onAction("question",data,option)}>{option}</button>)}</div>{data.answer != null && <small>你的选择：{String(data.answer)}</small>}</section>;
  if (message.kind === "authorization") return <section className="puck-card puck-authorization-card" role="group" aria-label="授权卡片"><strong>{terminal ? "授权已处理" : "需要授权"}</strong><p>{message.text}</p><dl><dt>动作</dt><dd>{String(data.action || "未说明")}</dd><dt>主机</dt><dd>{String(data.deviceId || "未说明")}</dd><dt>目录</dt><dd>{String(data.cwd || "未说明")}</dd><dt>有效期</dt><dd>{data.expiresAt ? new Date(String(data.expiresAt)).toLocaleString("zh-CN") : "未说明"}</dd></dl>{(data.risk != null || data.riskTags != null) && <small>风险：{String(data.risk || arrayData<string>(data.riskTags).join("、") || "无额外风险")}</small>}<div><button className="danger" type="button" disabled={terminal || submitting} onClick={()=>onAction("authorization",data,"reject")}>拒绝</button><button className="primary" type="button" disabled={terminal || submitting} onClick={()=>onAction("authorization",data,"approve_once")}>仅这一次允许</button></div></section>;
  if (message.kind === "task") return <section className="puck-card puck-task-card" role="group" aria-label="任务卡片"><strong>{message.text}</strong><span className="puck-state">{{running:"进行中",dispatching:"派发中",waiting_user:"等待回复",awaiting_authorization:"等待授权",stopped:"已停止",reconciling:"正在核对",done:"已完成",failed:"失败"}[String(data.status)] || String(data.status || "待处理")}</span><details><summary>{message.workSummary || "查看工作过程"}</summary><p>{String(data.summary || "执行细节按需展开，不在对话中刷屏。")}</p></details>{Boolean(data.deviceId && data.threadId) && <button type="button" onClick={()=>onAction("task",data,"open")}>打开会话</button>}</section>;
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
  const readyBrains = useMemo(() => devices.filter(device => device.capabilities?.puckBrainReady), [devices]);
  const selected = assistants.find(assistant => assistant.assistantId === selectedID);
  const puckState = messageState(messages);
  const busy = messages.some(message => message.kind === "processing" || (message.kind === "task" && ["running", "dispatching", "waiting_user", "awaiting_authorization"].includes(String(cardData(message).status))));

  async function loadList() {
    const response = await request("puck.list", "relay", {});
    if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
    const next = arrayData<Assistant>(response.data?.assistants);
    setAssistants(next); setSelectedID(current => current && next.some(item => item.assistantId === current) ? current : next[0]?.assistantId || "");
  }
  useEffect(() => { if (state === "ready") void loadList(); }, [state]);
  useEffect(() => {
    if (!selectedID) { setMessages([]); return; }
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
    setMessages([]);
  }
  async function send() {
    const text = draft.trim(); if (!selected || !text) return;
    if (["停", "停止"].includes(text)) { setDraft(""); await stop(); return; }
    const clientMessageId = crypto.randomUUID(); setDraft("");
    setMessages(current => [...current, { id: clientMessageId, role: "user", kind: "text", text }, { id: `${clientMessageId}-processing`, role: "assistant", kind: "processing", text: "正在处理…" }]);
    const response = await request("puck.message.send", "relay", { assistantId: selected.assistantId, clientMessageId, text });
    if (response.outcome !== "accepted") setMessages(current => current.map(item => item.id === `${clientMessageId}-processing` ? { ...item, kind: "error", text: responseError(response) } : item));
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

  if (state !== "ready") return <main className="puck-unavailable"><button type="button" onClick={onBack}>返回会话</button><div><span className="puck-mark">P</span><h1>Puck 不可用</h1><p>{reason === "PUCK_REQUIRES_PASSKEY_HTTPS" ? "Puck 仅支持 Passkey + HTTPS" : "管理员尚未开启 Puck。"}</p><small>现有 Codex 会话不受影响。</small></div></main>;
  return <main className="puck-workspace">
    {mobilePanel && <button className="puck-panel-backdrop" aria-label="关闭面板" onClick={()=>setMobilePanel("")}/>}<aside className={`puck-assistants ${mobilePanel === "assistants" ? "mobile-open" : ""}`}><header><b>Puck</b><button type="button" onClick={() => setCreating(true)} aria-label="新建助理">＋</button></header>{assistants.map(item => <button className={item.assistantId === selectedID ? "active" : ""} key={item.assistantId} onClick={() => { setSelectedID(item.assistantId); setMobilePanel(""); }}><PuckAvatar assistantId={item.assistantId} name={item.name} design={avatarDesign(item)} state={item.assistantId === selectedID ? puckState : "idle"} size="small" attention={item.assistantId === selectedID && ["waiting_user","awaiting_authorization","error"].includes(puckState)}/><span className="puck-assistant-copy"><strong>{item.name}</strong><small>{item.assistantId === selectedID && messages.at(-1)?.text || (item.status === "ready" ? "在线" : "准备中")}</small></span></button>)}<button className="puck-back" type="button" onClick={onBack}>← Codex 会话</button></aside>
    <section className="puck-conversation">
      <header><button className="puck-mobile-menu puck-icon-button" type="button" aria-label="打开助理列表" onClick={()=>setMobilePanel("assistants")}>☰</button>{selected && <PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState}/>}<div><strong>{selected?.name || "Puck"}</strong><small>{selected ? "你的个人助理" : "创建一个助理开始"}</small></div>{selected && <div className="puck-header-actions"><button className="puck-mobile-menu puck-icon-button" type="button" aria-label="查看助理信息" onClick={()=>setMobilePanel("info")}>ⓘ</button>{busy && <button className="puck-stop" type="button" aria-label="停止全部工作" onClick={() => void stop()}><span aria-hidden="true">■</span></button>}</div>}</header>
      <div className="puck-transcript" aria-live="polite">{assistants.length === 0 ? <div className="puck-empty"><span className="puck-mark">P</span><h2>创建你的第一个助理</h2><p>给它一个名字，选择在哪台设备上思考。</p><button className="primary" type="button" aria-label="新建助理" onClick={() => setCreating(true)}>新建助理</button></div> : messages.map(message => <article key={message.id} className={`puck-message ${message.role} ${message.kind}`}>{message.role === "assistant" && selected && <PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState} size="small"/>}<div className="puck-message-content"><Card message={message} submitting={submittingCard === String(cardData(message).authorizationId || cardData(message).questionId || "")} onAction={(kind,data,value)=>void respondCard(kind,data,value)}/>{!["question", "authorization", "task"].includes(message.kind) && <div className="puck-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{message.text}</ReactMarkdown></div>}{message.createdAt && <time>{new Date(message.createdAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", timeZone: "Asia/Shanghai" })}</time>}</div></article>)}</div>
      {notice && <div className="puck-notice" role="alert">{notice}<button onClick={() => setNotice("")}>关闭</button></div>}
      {selected && <footer><textarea aria-label="给 Puck 发消息" value={draft} onChange={event => setDraft(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void send(); } }} placeholder="说说你想做什么…"/><button className="primary puck-send" type="button" aria-label="发送" onClick={() => void send()} disabled={!draft.trim()}><span aria-hidden="true">↑</span></button></footer>}
    </section>
    <aside className={`puck-info ${mobilePanel === "info" ? "mobile-open" : ""}`}><h3>助理信息</h3>{selected ? <><div className="puck-avatar-editor"><PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState} size="large" attention={["waiting_user","awaiting_authorization","error"].includes(puckState)}/><div className="puck-avatar-picker"><span className="puck-avatar-picker-label">形状</span><div className="puck-shape-options" role="radiogroup" aria-label="头像形状">{avatarShapes.map(option => { const checked = avatarDesign(selected).shape === option.value; return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-shape-option ${checked ? "selected" : ""}`} key={option.value} onClick={()=>void updateAvatar(option.value,avatarDesign(selected).color)} onKeyDown={event=>moveRadio(event,avatarShapes.map(item=>item.value),avatarDesign(selected).shape,value=>void updateAvatar(value as AvatarShape,avatarDesign(selected).color))}><PuckAvatar assistantId={`${selected.assistantId}-${option.value}`} name={option.label} design={{shape:option.value,color:avatarDesign(selected).color}} size="small"/><span className="puck-avatar-choice-check" aria-hidden="true">✓</span></button>; })}</div><span className="puck-avatar-picker-label">颜色</span><div className="puck-color-options" role="radiogroup" aria-label="头像颜色">{avatarColors.map(option => { const checked = avatarDesign(selected).color === option.value; return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-color-option ${checked ? "selected" : ""}`} style={{"--puck-choice-color":option.value} as CSSProperties} key={option.value} onClick={()=>void updateAvatar(avatarDesign(selected).shape,option.value)} onKeyDown={event=>moveRadio(event,avatarColors.map(item=>item.value),avatarDesign(selected).color,value=>void updateAvatar(avatarDesign(selected).shape,value))}><span aria-hidden="true"/><span className="puck-avatar-choice-check" aria-hidden="true">✓</span></button>; })}</div></div></div><dl><dt>名称</dt><dd>{selected.name}</dd><dt>状态</dt><dd>{selected.status === "ready" ? "可用" : "准备中"}</dd><dt>大脑主机</dt><dd>{devices.find(device => device.deviceId === selected.brainDeviceId)?.name || selected.brainDeviceId}</dd></dl><p>{selected.description || "尚未添加说明。"}</p></> : <p>选择助理后查看设置。</p>}</aside>
    {creating && <div className="puck-dialog-backdrop" role="presentation"><form className="puck-dialog" onSubmit={event => { event.preventDefault(); void createAssistant(); }}><h2>新建助理</h2><label>助理名称<input aria-label="助理名称" value={name} onChange={event => setName(event.target.value)} autoFocus maxLength={80}/></label><label>大脑主机<select aria-label="大脑主机" value={brainDeviceID} onChange={event => setBrainDeviceID(event.target.value)}><option value="">请选择</option>{readyBrains.map(device => <option key={device.deviceId} value={device.deviceId}>{device.name || device.deviceId}</option>)}</select></label><div><button type="button" onClick={() => setCreating(false)}>取消</button><button className="primary" type="submit">创建</button></div></form></div>}
  </main>;
}
