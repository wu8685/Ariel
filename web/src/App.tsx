import { useEffect, useMemo, useRef, useState } from "react";
import { ArielSocket, type ConnectionStatus } from "./client";
import { applyThreadEvent, belongsToSubscription, keepOfflineDevice, preserveDraftAfterSend, recoveryTarget, type ThreadView } from "./state";
import { answersForSubmission } from "./interaction";
import type { ArielProtocolV1Envelope, Thread, Response, Interaction } from "./generated/protocol";
import "./interaction.css";

type Device = { deviceId: string; deviceName: string; agentOnline: boolean; codexReady: boolean; agentEpoch?: string; adapterVersion?: string; capabilities: { autoLoad: boolean; history?: boolean; send?: boolean; interrupt?: boolean; interaction?: boolean } };
const wsURL = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws`;
const resultText: Record<string, string> = { DEVICE_OFFLINE: "设备离线，请确认电脑上的 Agent 已连接。", TURN_BUSY: "这个会话正在运行；草稿已保留，不会自动重发。", STALE_TURN: "运行中的 turn 已变化，请刷新状态后再停止。", STALE_INTERACTION: "这项交互已经变化或过期，请查看最新会话状态。", OUTCOME_UNKNOWN: "执行结果不确定。请先查看会话状态，不要直接重发。", RESYNC_REQUIRED: "事件顺序发生变化，正在重新同步。", NATIVE_STATE_UNCERTAIN: "Codex 原生会话状态暂时无法确认，已停止此会话的远程操作。请稍后手动重新选择；若持续出现，请在电脑端查看。", HISTORY_TOO_LARGE: "会话历史超过安全传输上限，未截断内容。请在电脑端查看。", INTERACTION_UNSUPPORTED: "这张卡片已失效或当前决定不可用。", INVALID_ARGUMENT: "请求内容无效。", OVERLOADED: "请求过多，请稍后再试。" };

function errorText(response: Response): string {
  return response.error ? resultText[response.error.code] || response.error.message : "操作未完成。";
}

export function App() {
  const client = useMemo(() => new ArielSocket(wsURL), []);
  const [token, setToken] = useState("");
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const [devices, setDevices] = useState<Device[]>([]);
  const [deviceId, setDeviceId] = useState("");
  const [threads, setThreads] = useState<Thread[]>([]);
  const [cursor, setCursor] = useState("");
  const [threadId, setThreadId] = useState("");
  const [view, setView] = useState<ThreadView | null>(null);
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  const [working, setWorking] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [showList, setShowList] = useState(true);
  const [answers, setAnswers] = useState<Record<string, Record<string, string>>>({});
  const selection = useRef({ deviceId: "", threadId: "", view: null as ThreadView | null });
  const expectedSubscription = useRef("");
  const blockedSelection = useRef("");
  const pendingSelect = useRef(0);
  const resuming = useRef(false);
  const endRef = useRef<HTMLDivElement>(null);
  const device = devices.find(d => d.deviceId === deviceId);
  const mock = device?.adapterVersion?.startsWith("mock-") ?? false;
  const activeTurn = view?.thread.turns.findLast(t => t.status === "inProgress");

  async function refreshDevices(): Promise<Device[]> {
    const response = await client.request("device.list", "relay", {});
    if (response.outcome !== "accepted") { setNotice(errorText(response)); return []; }
    const found = (response.data?.devices as Device[] | undefined) || [];
    setDevices(previous => keepOfflineDevice(found, previous, selection.current.deviceId));
    setDeviceId(current => current || found[0]?.deviceId || "");
    return found;
  }

  async function loadThreads(id: string, next = "") {
    if (!id) { setThreads([]); return; }
    const response = await client.request("thread.list", id, { limit: 50, ...(next ? { cursor: next } : {}) });
    if (response.outcome !== "accepted") { setNotice(errorText(response)); return; }
    const page = (response.data?.threads as Thread[] | undefined) || [];
    setThreads(current => next ? [...current, ...page] : page);
    setCursor(String(response.data?.nextCursor || ""));
  }

  async function selectThread(id: string, targetDevice = deviceId) {
    if (!targetDevice) return;
    blockedSelection.current = "";
    const epoch = ++pendingSelect.current;
    const old = selection.current.view;
    const oldDevice = selection.current.deviceId;
    const oldSubscription = expectedSubscription.current;
    expectedSubscription.current = "";
    selection.current = { deviceId: targetDevice, threadId: id, view: null };
    setThreadId(id); setView(null); setNotice(""); setShowList(false);
    if (oldSubscription) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    else if (old) void client.request("thread.unsubscribe", old.deviceId, { subscriptionId: old.subscriptionId });
    const response = await client.request("thread.subscribe", targetDevice, { threadId: id });
    if (epoch !== pendingSelect.current) {
      const staleID = response.data?.subscriptionId;
      if (response.outcome === "accepted" && typeof staleID === "string") void client.request("thread.unsubscribe", targetDevice, { subscriptionId: staleID });
      return;
    }
    if (response.outcome !== "accepted") {
      if (response.error?.code === "NATIVE_STATE_UNCERTAIN") blockedSelection.current = `${targetDevice}\u0000${id}`;
      setNotice(errorText(response)); return;
    }
    const subID = response.data?.subscriptionId;
    if (typeof subID !== "string" || !subID) { setNotice("订阅回执缺少 ID，无法确认会话状态。"); return; }
    expectedSubscription.current = subID;
  }

  async function resumeSelected(online: Device[]) {
    const current = selection.current;
    const target = recoveryTarget(current.deviceId, current.threadId, online, blockedSelection.current);
    if (!target || current.view || resuming.current) return;
    resuming.current = true;
    try { await selectThread(target.threadId, target.deviceId); }
    finally { resuming.current = false; }
  }

  async function resync() {
    const current = selection.current;
    if (!current.threadId) return;
    setNotice("事件顺序中断，正在重新同步会话…");
    await selectThread(current.threadId);
  }

  useEffect(() => {
    client.onStatus = next => { setStatus(next); if (next !== "ready") { pendingSelect.current++; expectedSubscription.current = ""; selection.current.view = null; setView(null); } };
    client.onReady = () => { expectedSubscription.current = ""; selection.current.view = null; setView(null); void refreshDevices().then(online => void resumeSelected(online)); };
    client.onEvent = (event: ArielProtocolV1Envelope) => {
      if (event.type !== "event") return;
      if (event.event === "device.status") {
        if (!event.agentOnline && event.deviceId === selection.current.deviceId) {
          selection.current.view = null;
          setView(null);
          setNotice("Desktop Agent 暂时离线，恢复后会重新同步当前会话。");
        }
        void refreshDevices().then(online => void resumeSelected(online));
        return;
      }
      if (event.event === "interaction.resolved") {
        if (event.threadId === selection.current.threadId) setNotice("交互已完成，会话状态已同步。");
        return;
      }
      const current = selection.current;
      if (!belongsToSubscription(event, current.deviceId, current.threadId, expectedSubscription.current)) return;
      if (event.event === "thread.error") {
        if (!current.view || (current.view.subscriptionId === event.subscriptionId && current.view.streamId === event.streamId)) {
          if (event.code === "NATIVE_STATE_UNCERTAIN") blockedSelection.current = `${current.deviceId}\u0000${current.threadId}`;
          expectedSubscription.current = "";
          selection.current.view = null;
          setView(null);
          if (event.code === "RESYNC_REQUIRED") void selectThread(current.threadId, current.deviceId);
          else setNotice(resultText[event.code]);
        }
        return;
      }
      if (event.event === "thread.snapshot") {
        const next = applyThreadEvent(null, event);
        selection.current.view = next; setView(next); setNotice("");
      } else if (event.event === "thread.update") {
        const next = applyThreadEvent(current.view, event);
        if (!next) { void resync(); return; }
        selection.current.view = next; setView(next);
        setThreads(list => list.map(t => t.threadId === next.threadId ? next.thread : t));
      }
    };
    return () => client.disconnect();
  }, [client]);

  useEffect(() => {
    pendingSelect.current++;
    const oldDevice = selection.current.deviceId;
    const oldSubscription = expectedSubscription.current;
    if (oldDevice && oldSubscription) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    expectedSubscription.current = "";
    selection.current = { deviceId, threadId: "", view: null };
    setThreadId(""); setView(null); setThreads([]); setCursor("");
  }, [deviceId]);

  useEffect(() => {
    if (status === "ready" && deviceId) void loadThreads(deviceId);
  }, [deviceId, status]);

  useEffect(() => {
    let hiddenAt = 0;
    const onVisibility = () => {
      if (document.visibilityState === "hidden") hiddenAt = Date.now();
      else if (hiddenAt && Date.now() - hiddenAt >= 5000) {
        hiddenAt = 0;
        client.wake();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [client]);

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: "smooth", block: "end" }); }, [view?.seq]);

  async function send() {
    if (!view || !deviceId || !draft.trim() || working || view.thread.runtime !== "idle") return;
    const text = draft;
    setWorking(true); setNotice("");
    const response = await client.request("turn.start", deviceId, { threadId: view.threadId, clientMessageId: crypto.randomUUID(), text });
    setWorking(false);
    setDraft(current => current === text ? preserveDraftAfterSend(current, response.outcome) : current);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  async function stop() {
    if (!view || !activeTurn || stopping) return;
    setStopping(true);
    const response = await client.request("turn.interrupt", deviceId, { threadId: view.threadId, expectedTurnId: activeTurn.turnId });
    setStopping(false);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  async function respond(interaction: Interaction, decision: string) {
    if (!view || working) return;
    const raw = answers[interaction.interactionId] || {};
    const mapped = decision === "answer" ? answersForSubmission(interaction, raw) : null;
    if (decision === "answer" && !mapped) return;
    setWorking(true);
    const response = await client.request("interaction.respond", deviceId, { threadId: view.threadId, interactionId: interaction.interactionId, decision, ...(decision === "answer" ? { answers: mapped } : {}) });
    setWorking(false);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  return <div className="app-shell">
    <header className="masthead">
      <div className="brand"><span className="brand-mark" aria-hidden="true">✳</span><span>Ariel</span><small>Codex 随身工作台</small></div>
      <div className="mast-actions"><span className={`connection ${status}`}><span className="status-dot" />{status === "ready" ? "Relay 已连接" : status === "connecting" ? "正在连接" : "Relay 未连接"}</span><button className="text-button" onClick={() => client.disconnect()}>断开</button></div>
    </header>
    {status !== "ready" && <section className="connect-panel" aria-label="连接 Relay"><div><span className="eyebrow">PRIVATE ACCESS</span><h1>继续你的工作，<br />不必守在电脑前。</h1><p>输入本机 Relay token。它只保存在当前页面内存，不会写入浏览器存储。</p></div><form onSubmit={e => { e.preventDefault(); client.connect(token.trim()); }}><label htmlFor="token">连接口令</label><div className="connect-row"><input id="token" type="password" value={token} onChange={e => setToken(e.target.value)} autoComplete="off" placeholder="输入 Relay token" required /><button className="primary" type="submit">连接 <span aria-hidden="true">↗</span></button></div><small>仅建议在可信局域网使用。HTTP/WS 连接未加密。</small></form></section>}
    <div className="workspace">
      <aside className={`sidebar ${showList ? "open" : ""}`} aria-label="会话列表">
        <div className="sidebar-head"><span className="eyebrow">WORKSPACE</span><h2>会话</h2><button className="icon-button mobile-close" aria-label="关闭会话列表" onClick={() => setShowList(false)}>×</button><button className="icon-button" aria-label="刷新会话" onClick={() => void loadThreads(deviceId)} disabled={!deviceId}>↻</button></div>
        <label className="device-label" htmlFor="device">设备</label><select id="device" value={deviceId} onChange={e => setDeviceId(e.target.value)} disabled={status !== "ready"}><option value="">{devices.length ? "选择设备" : "暂无在线设备"}</option>{devices.map(d => <option key={d.deviceId} value={d.deviceId}>{d.deviceName}</option>)}</select>
        {device && <div className="device-meta"><span className={`status-dot ${device.agentOnline ? "online" : ""}`} />{device.agentOnline ? "Agent 在线" : "Agent 离线"}<span>·</span>{device.codexReady ? "Codex 就绪" : mock ? "Mock 演示" : "Codex 未就绪"}</div>}
        <div className="list-caption"><span>最近会话</span><span>{threads.length}</span></div>
        <div className="thread-list">{threads.map(t => <button key={t.threadId} className={`thread-row ${threadId === t.threadId ? "selected" : ""}`} onClick={() => void selectThread(t.threadId)}><span className="thread-title">{t.title || "未命名会话"}</span><span className="thread-path">{t.cwd}</span><span className="thread-date">{new Date(t.updatedAt).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })}</span></button>)}</div>
        {cursor && <button className="load-more" onClick={() => void loadThreads(deviceId, cursor)}>加载更多 →</button>}
        <div className="sidebar-foot">{mock ? "MOCK SESSION · 非真实 Codex 历史" : "原始会话 · 不创建远程副本"}</div>
      </aside>
      <main className="conversation">
        <div className="conversation-head"><button className="mobile-list text-button" onClick={() => setShowList(true)}>☰ 会话</button><div><span className="eyebrow">{mock ? "MOCK DEMO" : "CODEX SESSION"}</span><h2>{view?.thread.title || threads.find(t => t.threadId === threadId)?.title || "选择一个会话"}</h2><span className="head-path">{view?.thread.cwd || "从左侧选择历史会话，接着工作。"}</span></div><div className="head-right">{mock && <span className="mock-badge">模拟环境</span>}{view && <span className="runtime">{view.thread.runtime === "inProgress" ? "运行中" : view.thread.runtime === "idle" ? "待命" : view.thread.runtime === "notLoaded" ? "加载中" : "状态未知"}</span>}</div></div>
        <div className="transcript" aria-live="polite">{!view && <div className="empty"><div className="empty-symbol">✳</div><h3>{blockedSelection.current === `${deviceId}\u0000${threadId}` ? "会话状态无法确认" : threadId ? "正在同步会话…" : "从这里接续"}</h3><p>{blockedSelection.current === `${deviceId}\u0000${threadId}` ? "远程操作已暂停。请稍后手动重新选择会话。" : threadId ? "等待电脑端加载原始历史。" : "选一个会话，历史、运行状态与需要你决定的问题会出现在这里。"}</p></div>}{view?.thread.turns.map(turn => <section className="turn" key={turn.turnId}><div className="turn-status">{turn.status === "inProgress" ? "正在生成" : turn.status === "completed" ? "已完成" : turn.status === "interrupted" ? "已停止" : "失败"}</div>{turn.items.map(item => <article className={`message ${item.role}`} key={item.itemId}><div className="avatar">{item.role === "user" ? "你" : item.role === "assistant" ? "✳" : "i"}</div><div className="message-body"><div className="message-role">{item.role === "user" ? "你" : item.role === "assistant" ? "Codex" : "系统"}</div><div className="message-text">{item.text || (turn.status === "inProgress" && item.role === "assistant" ? <span className="thinking">正在思考…</span> : "")}</div></div></article>)}</section>)}{view?.thread.pendingInteractions.map(card => <InteractionCard key={card.interactionId} card={card} values={answers[card.interactionId] || {}} onChange={(id, value) => setAnswers(all => ({ ...all, [card.interactionId]: { ...all[card.interactionId], [id]: value } }))} onRespond={decision => void respond(card, decision)} disabled={working} />)}<div ref={endRef} /></div>
        <div className="composer-wrap">{notice && <div className="notice" role="alert"><span>!</span>{notice}<button aria-label="关闭提示" onClick={() => setNotice("")}>×</button></div>}<div className="composer"><textarea aria-label="发送消息" placeholder={view ? view.thread.runtime === "inProgress" ? "Codex 正在运行；你可以先写草稿…" : "给 Codex 发消息…" : "选择会话后开始输入…"} value={draft} onChange={e => setDraft(e.target.value)} onKeyDown={e => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(); } }} disabled={!view || status !== "ready"} rows={3} /><div className="composer-actions"><span>Enter 发送 · Shift+Enter 换行</span><div>{activeTurn && <button className="stop-button" onClick={() => void stop()} disabled={stopping || status !== "ready"}>■ 停止</button>}<button className="primary send-button" onClick={() => void send()} disabled={!view || status !== "ready" || working || !draft.trim() || view.thread.runtime !== "idle"}>发送 <span aria-hidden="true">↗</span></button></div></div></div></div>
      </main>
    </div>
  </div>;
}

function InteractionCard({ card, values, onChange, onRespond, disabled }: { card: Interaction; values: Record<string, string>; onChange: (id: string, value: string) => void; onRespond: (decision: string) => void; disabled: boolean }) {
  return <section className="interaction-card"><span className="eyebrow">NEEDS YOUR INPUT</span><h3>{card.kind === "command_approval" ? "等待命令审批" : card.kind === "file_approval" ? "等待文件变更审批" : card.kind === "permission_request" ? "等待权限请求" : card.kind === "user_input" ? "Codex 有一个问题" : "暂不支持的交互"}</h3><p>{card.prompt}</p>{card.questions?.map(q => <label key={q.id} className="question">{q.question}<input list={`options-${card.interactionId}-${q.id}`} value={values[q.id] || ""} onChange={e => onChange(q.id, e.target.value)} placeholder={q.options?.length ? "选择建议或自行输入" : "输入回答"} /><datalist id={`options-${card.interactionId}-${q.id}`}>{q.options?.map(o => <option key={o} value={o} />)}</datalist></label>)}<div className="interaction-actions">{card.availableDecisions.map(decision => <button key={decision} className={decision === "accept_once" || decision === "answer" ? "primary" : "secondary"} disabled={disabled || (decision === "answer" && !answersForSubmission(card, values))} onClick={() => onRespond(decision)}>{decision === "accept_once" && card.kind === "permission_request" ? "仅本轮按原请求授权" : ({ accept_once: "仅本次允许", deny: "拒绝", deny_and_stop: "拒绝并停止", answer: "提交回答" } as Record<string, string>)[decision]}</button>)}</div></section>;
}
