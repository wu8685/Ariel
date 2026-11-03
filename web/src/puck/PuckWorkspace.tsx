import { useEffect, useMemo, useState } from "react";
import "./puck.css";

type PuckResponse = { outcome: string; data?: Record<string, unknown>; error?: { code?: string; message?: string } };
type Request = (method: string, target: string, params: Record<string, unknown>) => Promise<PuckResponse>;
type Device = { deviceId: string; name?: string; capabilities?: Record<string, boolean> };
type Assistant = { assistantId: string; name: string; description?: string; brainDeviceId: string; status: string };
type Message = { id: string; role: string; kind: string; text: string; cardJSON?: string; createdAt?: string };

function responseError(response: PuckResponse): string {
  return response.error?.message || response.error?.code || "请求失败，请稍后重试。";
}
function arrayData<T>(value: unknown): T[] { return Array.isArray(value) ? value as T[] : []; }
function normalizeMessage(value: Record<string, unknown>): Message {
  return {
    id: String(value.id ?? value.ID ?? crypto.randomUUID()), role: String(value.role ?? value.Role ?? "assistant"),
    kind: String(value.kind ?? value.Kind ?? "text"), text: String(value.text ?? value.Text ?? ""),
    cardJSON: String(value.cardJSON ?? value.CardJSON ?? ""), createdAt: String(value.createdAt ?? value.CreatedAt ?? ""),
  };
}
function cardData(message: Message): Record<string, unknown> {
  try { return message.cardJSON ? JSON.parse(message.cardJSON) as Record<string, unknown> : {}; } catch { return {}; }
}
function Card({ message }: { message: Message }) {
  const data = cardData(message);
  if (message.kind === "question") return <section className="puck-card question" role="group" aria-label="问题卡片"><strong>需要你决定</strong><p>{message.text}</p><div className="puck-options">{arrayData<string>(data.options).map(option => <button key={option} type="button">{option}</button>)}</div></section>;
  if (message.kind === "authorization") return <section className="puck-card authorization" role="group" aria-label="授权卡片"><strong>需要授权</strong><p>{message.text}</p>{data.risk != null && <small>风险：{String(data.risk)}</small>}<div><button type="button">拒绝</button><button className="primary" type="button">仅这一次允许</button></div></section>;
  if (message.kind === "task") return <section className="puck-card task" role="group" aria-label="任务卡片"><strong>{message.text}</strong><span className="puck-state">{data.status === "running" ? "进行中" : String(data.status || "待处理")}</span><details><summary>查看工作过程</summary><p>执行细节按需展开，不在对话中刷屏。</p></details></section>;
  return null;
}

export function PuckWorkspace({ request, state, reason, devices, onBack }: { request: Request; state: "ready" | "unavailable" | "disabled"; reason?: string; devices: Device[]; onBack?: () => void }) {
  const [assistants, setAssistants] = useState<Assistant[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [brainDeviceID, setBrainDeviceID] = useState("");
	const [mobilePanel, setMobilePanel] = useState<"" | "assistants" | "info">("");
  const readyBrains = useMemo(() => devices.filter(device => device.capabilities?.puckBrainReady), [devices]);
  const selected = assistants.find(assistant => assistant.assistantId === selectedID);

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
      if (active && response.outcome === "accepted") setMessages(arrayData<Record<string, unknown>>(response.data?.messages).map(normalizeMessage));
    });
    return () => { active = false; };
  }, [selectedID]);
	useEffect(() => {
		if (!selectedID) return;
		const timer = window.setInterval(() => { void request("puck.history", "relay", { assistantId: selectedID }).then(response => {
			if (response.outcome !== "accepted") return;
			const durable = arrayData<Record<string, unknown>>(response.data?.messages).map(normalizeMessage);
			setMessages(current => durable.length ? [...durable, ...current.filter(message => message.kind === "processing" || message.kind === "acknowledged").filter(local => !durable.some(saved => saved.id === local.id.replace(/-processing$/, "")))] : current);
		}); }, 2000);
		return () => window.clearInterval(timer);
	}, [selectedID]);

  async function createAssistant() {
    const deviceID = brainDeviceID || readyBrains[0]?.deviceId;
    if (!name.trim() || !deviceID) { setNotice("请填写名称并选择可用的大脑主机。"); return; }
    const response = await request("puck.create", "relay", { name: name.trim(), brainDeviceId: deviceID, allowedScope: {} });
    if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
    const assistant = response.data?.assistant as Assistant;
    setAssistants(current => [...current, assistant]); setSelectedID(assistant.assistantId); setName(""); setCreating(false);
    setMessages([{ id: crypto.randomUUID(), role: "assistant", kind: "text", text: `你好，我是 ${assistant.name}。有什么想先处理的？` }]);
  }
  async function send() {
    const text = draft.trim(); if (!selected || !text) return;
    const clientMessageId = crypto.randomUUID(); setDraft("");
    setMessages(current => [...current, { id: clientMessageId, role: "user", kind: "text", text }, { id: `${clientMessageId}-processing`, role: "assistant", kind: "processing", text: "正在处理…" }]);
    const response = await request("puck.message.send", "relay", { assistantId: selected.assistantId, clientMessageId, text });
    if (response.outcome !== "accepted") setMessages(current => current.map(item => item.id === `${clientMessageId}-processing` ? { ...item, kind: "error", text: responseError(response) } : item));
	else window.setTimeout(() => setMessages(current => current.map(item => item.id === `${clientMessageId}-processing` && item.kind === "processing" ? { ...item, kind: "acknowledged", text: "收到，正在处理。" } : item)), Number(response.data?.templateAfterMs || 3000));
  }
  async function stop() {
    if (!selected) return;
    const response = await request("puck.stop", "relay", { assistantId: selected.assistantId });
    setMessages(current => [...current, { id: crypto.randomUUID(), role: "assistant", kind: "text", text: response.outcome === "accepted" ? String(response.data?.summary || "已请求停止") : responseError(response) }]);
  }

  if (state !== "ready") return <main className="puck-unavailable"><button type="button" onClick={onBack}>返回会话</button><div><span className="puck-mark">P</span><h1>Puck 不可用</h1><p>{reason === "PUCK_REQUIRES_PASSKEY_HTTPS" ? "Puck 仅支持 Passkey + HTTPS" : "管理员尚未开启 Puck。"}</p><small>现有 Codex 会话不受影响。</small></div></main>;
  return <main className="puck-workspace">
    {mobilePanel && <button className="puck-panel-backdrop" aria-label="关闭面板" onClick={()=>setMobilePanel("")}/>}<aside className={`puck-assistants ${mobilePanel === "assistants" ? "mobile-open" : ""}`}><header><b>Puck</b><button type="button" onClick={() => setCreating(true)} aria-label="新建助理">＋</button></header>{assistants.map(item => <button className={item.assistantId === selectedID ? "active" : ""} key={item.assistantId} onClick={() => { setSelectedID(item.assistantId); setMobilePanel(""); }}><span>{item.name.slice(0, 1)}</span><span><strong>{item.name}</strong><small>{item.status === "ready" ? "在线" : "准备中"}</small></span></button>)}<button className="puck-back" type="button" onClick={onBack}>← Codex 会话</button></aside>
    <section className="puck-conversation">
      <header><button className="puck-mobile-menu" type="button" aria-label="打开助理列表" onClick={()=>setMobilePanel("assistants")}>☰</button><div><strong>{selected?.name || "Puck"}</strong><small>{selected ? "你的个人助理" : "创建一个助理开始"}</small></div>{selected && <div className="puck-header-actions"><button className="puck-mobile-menu" type="button" aria-label="查看助理信息" onClick={()=>setMobilePanel("info")}>ⓘ</button><button type="button" aria-label="停止全部工作" onClick={() => void stop()}>停止</button></div>}</header>
      <div className="puck-transcript" aria-live="polite">{assistants.length === 0 ? <div className="puck-empty"><span className="puck-mark">P</span><h2>创建你的第一个助理</h2><p>给它一个名字，选择在哪台设备上思考。</p><button className="primary" type="button" aria-label="新建助理" onClick={() => setCreating(true)}>新建助理</button></div> : messages.map(message => <article key={message.id} className={`puck-message ${message.role} ${message.kind}`}><Card message={message}/>{!["question", "authorization", "task"].includes(message.kind) && <p>{message.text}</p>}{message.createdAt && <time>{new Date(message.createdAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", timeZone: "Asia/Shanghai" })}</time>}</article>)}</div>
      {notice && <div className="puck-notice" role="alert">{notice}<button onClick={() => setNotice("")}>关闭</button></div>}
      {selected && <footer><textarea aria-label="给 Puck 发消息" value={draft} onChange={event => setDraft(event.target.value)} placeholder="说说你想做什么…"/><button className="primary" type="button" aria-label="发送" onClick={() => void send()} disabled={!draft.trim()}>发送</button></footer>}
    </section>
    <aside className={`puck-info ${mobilePanel === "info" ? "mobile-open" : ""}`}><h3>助理信息</h3>{selected ? <><dl><dt>名称</dt><dd>{selected.name}</dd><dt>状态</dt><dd>{selected.status === "ready" ? "可用" : "准备中"}</dd><dt>大脑主机</dt><dd>{devices.find(device => device.deviceId === selected.brainDeviceId)?.name || selected.brainDeviceId}</dd></dl><p>{selected.description || "尚未添加说明。"}</p></> : <p>选择助理后查看设置。</p>}</aside>
    {creating && <div className="puck-dialog-backdrop" role="presentation"><form className="puck-dialog" onSubmit={event => { event.preventDefault(); void createAssistant(); }}><h2>新建助理</h2><label>助理名称<input aria-label="助理名称" value={name} onChange={event => setName(event.target.value)} autoFocus maxLength={80}/></label><label>大脑主机<select aria-label="大脑主机" value={brainDeviceID} onChange={event => setBrainDeviceID(event.target.value)}><option value="">请选择</option>{readyBrains.map(device => <option key={device.deviceId} value={device.deviceId}>{device.name || device.deviceId}</option>)}</select></label><div><button type="button" onClick={() => setCreating(false)}>取消</button><button className="primary" type="submit">创建</button></div></form></div>}
  </main>;
}
