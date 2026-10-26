import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CircleButton, CloseGlyph } from "./circular-button";
import type { ArielSocket } from "./client";
import { newRequestID } from "./ids";
import "./execution-settings.css";
import { changeModel, defaultValues, effortLabel, speedLabel, parseCatalog, parseSettings, type ExecutionCatalog, type ExecutionGuard, type ExecutionSnapshot, type ExecutionValues, type ExecutionSettingsStore } from "./model-settings";
type Props = { client: ArielSocket; store: ExecutionSettingsStore; deviceId: string; agentEpoch: string; targetKey: string; threadId?: string; draft?: boolean; connected: boolean; ownerReady: boolean; queued: boolean; busy: boolean; hasImages: boolean; onState: (state: ExecutionGuard) => void };
const effortLevels: Record<string, number> = { none: 0, minimal: 1, low: 2, medium: 3, high: 4, xhigh: 5, max: 6, ultra: 7 };
function EffortGlyph({ effort }: { effort?: string }) {
  const level = effort === undefined ? undefined : effortLevels[effort];
  const state = effort === "" ? "inherit" : level === undefined ? "unknown" : effort;
  const label = `推理强度：${effort === undefined ? "未确认" : effortLabel(effort)}`;
  return <svg className="execution-glyph execution-effort" role="img" aria-label={label} data-icon-state={state} viewBox="0 0 20 20" fill="none">
    <title>{label}</title>
    {state === "inherit" ? <path d="M4 7h10l-3-3m5 9H6l3 3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/> : level === undefined ? <path d="M7 6.5a3 3 0 1 1 5 2.2c-1.4.7-2 1.3-2 2.8m0 3h.01" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/> : <>{Array.from({ length: 7 }, (_, i) => <rect key={i} x={2 + i * 2.4} y={16 - i * 1.6} width="1.5" height={2 + i * 1.6} rx=".6" fill="currentColor" opacity={i < level ? 1 : .2}/>)}{level === 0 && <path d="m3 4 14 14" stroke="currentColor" strokeWidth="1.2"/>}</>}
  </svg>;
}
function SpeedGlyph({ speed, model }: { speed?: ExecutionValues["speed"]; model?: ExecutionCatalog["models"][number] }) {
  const tier = model?.tiers.find(t => t.id === speed?.tierId);
  // Unknown native tiers remain custom, rather than guessing that every tier is Fast.
  const state = !speed ? "unknown" : speed.mode !== "tier" ? speed.mode : /^(fast|加速)$/i.test(tier?.name || "") ? "fast" : "custom";
  const label = `速度：${speed ? speedLabel(speed, model) : "未确认"}`;
  return <svg className="execution-glyph execution-speed" role="img" aria-label={label} data-icon-state={state} viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <title>{label}</title>
    {state === "fast" ? <path d="m11.5 2-7 9h5l-1 7 7-10h-5z"/> : state === "standard" ? <path d="m7 4 6 6-6 6"/> : state === "inherit" ? <path d="m8 7 2-2a3 3 0 0 1 4 4l-2 2m0 2-2 2a3 3 0 0 1-4-4l2-2m0 3 4-4"/> : state === "custom" ? <><path d="M3 5h14M3 10h14M3 15h14"/><path d="M7 3v4m6 1v4m-5 1v4"/></> : <path d="M7 6.5a3 3 0 1 1 5 2.2c-1.4.7-2 1.3-2 2.8m0 3h.01"/>}
  </svg>;
}
export function ExecutionSettingsControl(p: Props) {
  const summaryId = useId();
  const identity = `${p.deviceId}\0${p.agentEpoch}\0${p.targetKey}`;
  const [catalog, setCatalog] = useState<ExecutionCatalog | null>(null), [snapshot, setSnapshot] = useState<ExecutionSnapshot | null>(() => p.store.snapshots.get(identity) || null);
  const [draftValues, setDraftValues] = useState(() => p.store.drafts.get(identity));
  const [supported, setSupported] = useState<boolean | null>(()=>p.store.snapshots.has(identity)||p.store.drafts.has(identity)?true:null), [watchReady, setWatchReady] = useState(false);
  const [open, setOpen] = useState(false), [editing, setEditing] = useState<ExecutionValues | null>(null);
  const [editingRevision,setEditingRevision]=useState("");
  const [saving, setSaving] = useState(false), [uncertain, setUncertain] = useState(false), [error, setError] = useState(""), [adjustment, setAdjustment] = useState(""), [reload, setReload] = useState(0);
  const dialog = useRef<HTMLDivElement>(null), trigger = useRef<HTMLButtonElement>(null), callback = useRef(p.onState); callback.current = p.onState;
  const currentEpoch = useRef(0), eventVersion = useRef(0);
  const latestSnapshot=useRef(snapshot);latestSnapshot.current=snapshot;
  const currentValues = p.draft ? draftValues : snapshot?.values, model = catalog?.models.find(m => m.id === currentValues?.model);
  const imagesBlocked = Boolean(p.hasImages && model && !model.inputModalities.includes("image"));
  const writable = p.connected && p.ownerReady && !p.busy && !p.queued && !saving && !uncertain && (p.draft || watchReady && snapshot?.writable && snapshot.freshness === "fresh");
  useEffect(() => { callback.current({ key: p.targetKey, busy: saving || uncertain, imagesBlocked, editSupported:catalog?.editSupported === true, ...(p.draft && draftValues ? { draftSettings: draftValues } : {}) }); }, [p.targetKey, p.draft, saving, uncertain, imagesBlocked, draftValues, catalog?.editSupported]);
  useEffect(() => {
    let cancelled = false; if (!p.connected || !p.deviceId) return;if(!catalog)setSupported(null);
    const key = `${p.client.sessionGeneration}\0${p.deviceId}\0${p.agentEpoch}`;
    void p.store.catalog(key, async () => {
      const response = await p.client.request("model.catalog.read", p.deviceId, reload ? { refresh: true } : {});
      if (response.error?.code === "PROTOCOL_UNSUPPORTED"||response.outcome === "accepted" && response.data?.settingsSupported !== true) throw new Error("unsupported");
      if (response.outcome !== "accepted" || response.data?.agentEpoch !== p.agentEpoch || !response.data.settingsSupported) throw new Error("unavailable");
      const result=parseCatalog(response.data.catalog);if(!result)throw new Error("unavailable");return {...result,editSupported:response.data?.editSupported===true};
    }, reload > 0).then(c => { if (!cancelled&&c) { setCatalog(c); setSupported(true); } },reason=>{if(!cancelled){setSupported(reason instanceof Error&&reason.message==="unsupported"?false:true);setError("模型选项暂不可用，请重试。原会话不受影响。");}});
    return () => { cancelled = true; };
  }, [p.connected, p.deviceId, p.agentEpoch, p.client, p.store, reload]);
  useEffect(() => {
    const generation = ++currentEpoch.current; let cancelled = false, watch = { subscriptionId: "", streamId: "", seq: 1 }; setWatchReady(false);
    if (!p.connected || !p.ownerReady || !catalog || p.draft || !p.threadId) return;
    const apply = (next: ExecutionSnapshot) => { if (!cancelled) { p.store.keep(p.store.snapshots, identity, next); latestSnapshot.current=next;setSnapshot(next); setUncertain(false); } };
    const unsubscribe = p.client.subscribeEvents(event => {
      if (event.type !== "event" || event.event !== "thread.settings.update" || event.deviceId !== p.deviceId || event.agentEpoch !== p.agentEpoch || event.threadId !== p.threadId || event.subscriptionId !== watch.subscriptionId || event.streamId !== watch.streamId || event.seq <= watch.seq) return;
      const next = parseSettings(event.settings); if (!next) return; watch.seq = event.seq; eventVersion.current++; apply(next);
    });
    void p.client.request("thread.settings.watch", p.deviceId, { threadId: p.threadId }).then(response => {
      const data = response.data, next = parseSettings(data?.settings);
      if (response.outcome === "accepted" && typeof data?.subscriptionId === "string") {
        if (cancelled || generation !== currentEpoch.current) { void p.client.request("thread.settings.unwatch", p.deviceId, { subscriptionId: data.subscriptionId }); return; }
        if (next && data.agentEpoch === p.agentEpoch && typeof data.streamId === "string") { watch = { subscriptionId: data.subscriptionId, streamId: data.streamId, seq: 1 }; apply(next); setWatchReady(true); setError(""); if(reload>0){setEditing(next.values);setEditingRevision(next.revision);}return; }
        void p.client.request("thread.settings.unwatch",p.deviceId,{subscriptionId:data.subscriptionId});
      }
      if (!cancelled) setError("会话设置尚未确认，可稍后重新读取。");
    });
    return () => { cancelled = true; unsubscribe(); if (watch.subscriptionId) void p.client.request("thread.settings.unwatch", p.deviceId, { subscriptionId: watch.subscriptionId }); };
  }, [p.connected, p.ownerReady, p.deviceId, p.agentEpoch, p.threadId, p.draft, p.client, p.store, identity, catalog, reload]);
  function close() { if (!saving) { setOpen(false); trigger.current?.focus(); } }
  useEffect(() => {
    if (!open) return; dialog.current?.querySelector<HTMLElement>("select,button")?.focus();
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); if (!saving) { setOpen(false); trigger.current?.focus(); } }
      if (event.key !== "Tab") return;
      const nodes = [...(dialog.current?.querySelectorAll<HTMLElement>("button:not(:disabled),select:not(:disabled)") || [])], first = nodes[0], last = nodes.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); } else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }; document.addEventListener("keydown", keydown); return () => document.removeEventListener("keydown", keydown);
  }, [open, saving]);
  async function save() {
    if (!editing || !writable) return;
    if (p.draft) { p.store.keep(p.store.drafts, identity, editing); setDraftValues(editing); close(); return; }
    if (!snapshot || !p.threadId) return;
    const version = eventVersion.current, generation = currentEpoch.current; setSaving(true); setError("");
    const response = await p.client.request("thread.settings.update", p.deviceId, { threadId: p.threadId, expectedRevision: editingRevision, operationId: newRequestID(), patch: editing });
    setSaving(false); if (generation !== currentEpoch.current) return;
    const confirmed = parseSettings(response.data?.settings);
    if(eventVersion.current>version&&latestSnapshot.current?.freshness==="fresh"&&JSON.stringify(latestSnapshot.current.values)===JSON.stringify(editing)){setOpen(false);trigger.current?.focus();return;}
    if (response.outcome === "accepted" && confirmed && response.data?.agentEpoch === p.agentEpoch) { if (eventVersion.current === version) { setSnapshot(confirmed); p.store.keep(p.store.snapshots, identity, confirmed); } setOpen(false); trigger.current?.focus(); return; }
    setError(response.outcome === "unknown"||response.outcome==="accepted" ? "设置结果待确认；不会自动重试。请重新读取后核对。" : response.error?.code === "STALE_TURN" ? "Codex 中的设置已变化，请重新读取后选择。" : response.error?.code === "TURN_BUSY" ? "已有排队消息，请等待队列处理后修改。" : "设置未保存，原设置和草稿已保留。");
    if (response.outcome === "unknown" || response.outcome === "accepted") setUncertain(true); if (response.error?.code === "STALE_TURN") setWatchReady(false);
  }
  const selectedModel = catalog?.models.find(m => m.id === editing?.model);
  const modelLabel = model?.name || currentValues?.model || "继承 Codex";
  const summary = `模型：${modelLabel}；推理强度：${currentValues ? effortLabel(currentValues.effort) : "未确认"}；速度：${currentValues ? speedLabel(currentValues.speed, model) : "未确认"}${saving ? "；设置保存中" : ""}`;
  if (supported !== true || !p.targetKey) return null;
  return <div className="execution-options">
    <button type="button" className="execution-trigger" ref={trigger} aria-label="模型与推理设置" aria-describedby={summaryId} title={summary} aria-busy={saving} aria-expanded={open} onClick={() => { setEditing(currentValues || (catalog && defaultValues(catalog)));setEditingRevision(snapshot?.revision||""); setAdjustment(""); setOpen(true); }}><span className="execution-model">{modelLabel}</span><EffortGlyph effort={currentValues?.effort}/><SpeedGlyph speed={currentValues?.speed} model={model}/>{saving ? <svg className="execution-saving" aria-hidden="true" viewBox="0 0 20 20"><circle cx="10" cy="10" r="7" fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="32 12"/></svg> : <svg className="execution-chevron" aria-hidden="true" viewBox="0 0 16 16"><path d="m4 6 4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round"/></svg>}</button>
    <span id={summaryId} className="sr-only">{summary}</span>
    {open && createPortal(<div className="execution-backdrop" onClick={e => { if (e.target === e.currentTarget) close(); }}><div ref={dialog} role="dialog" aria-modal="true" aria-labelledby="execution-title" className="execution-dialog">
      <header><div><h3 id="execution-title">模型与推理设置</h3><p>仅影响当前会话的下一轮，正在执行的这一轮不变。</p></div><CircleButton type="button" aria-label="关闭模型设置" onClick={close} disabled={saving}><CloseGlyph/></CircleButton></header>
      {catalog && editing ? <fieldset disabled={!writable}>
        <label>模型<select aria-label="模型" value={editing.model} onChange={e => { const m = catalog.models.find(m => m.id === e.target.value); if (m) { const next = changeModel(editing, m); setEditing(next.values); setAdjustment(next.changedDefaults ? "原强度或速度不适用于此模型，已改为下方显示的默认选项；保存后生效。" : ""); } }}>{!selectedModel && <option value={editing.model}>{editing.model}（当前模型）</option>}{catalog.models.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></label><p>{selectedModel?.description}</p>
        <label>推理强度<select aria-label="推理强度" value={editing.effort} onChange={e => setEditing({ ...editing, effort: e.target.value })}><option value="">继承 Codex</option>{selectedModel?.efforts.map(e => <option value={e.id} key={e.id}>{effortLabel(e.id)}</option>)}</select></label><p>{selectedModel?.efforts.find(e => e.id === editing.effort)?.description}</p>
        <label>速度<select aria-label="速度" value={editing.speed.mode === "tier" ? `tier:${editing.speed.tierId}` : editing.speed.mode} onChange={e => setEditing({ ...editing, speed: e.target.value.startsWith("tier:") ? { mode: "tier", tierId: e.target.value.slice(5) } : { mode: e.target.value as "standard" | "inherit" } })}><option value="inherit">继承 Codex</option><option value="standard">标准</option>{selectedModel?.tiers.map(t => <option key={t.id} value={`tier:${t.id}`}>{t.name}</option>)}</select></label><p>{selectedModel?.tiers.find(t => t.id === editing.speed.tierId)?.description || "速度与推理强度是独立选项。"}</p>
      </fieldset> : <p>{supported === null ? "正在读取此设备的模型选项…" : "模型选项暂不可用。"}</p>}
      {adjustment && <p className="execution-hint" role="status">{adjustment}</p>}{p.queued && <p className="execution-hint">有排队消息，待队列处理后再修改。</p>}{!p.draft && !watchReady && <p className="execution-hint">当前设置未确认，只读展示。</p>}{error && <p role="alert" className="execution-error">{error}</p>}
      <footer><button type="button" disabled={saving || !p.connected} onClick={() => { setError(""); setReload(n => n + 1); }}>重新读取</button><button type="button" onClick={close} disabled={saving}>取消</button><button type="button" className="primary" disabled={!writable || !editing || !selectedModel} onClick={() => void save()}>{saving ? "保存中…" : "保存设置"}</button></footer>
    </div></div>, document.body)}
  </div>;
}
