import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ArielSocket } from "./client";
import { newRequestID } from "./ids";
import "./execution-settings.css";
import { changeModel, defaultValues, effortLabel, speedLabel, parseCatalog, parseSettings, type ExecutionCatalog, type ExecutionGuard, type ExecutionSnapshot, type ExecutionValues, type ExecutionSettingsStore } from "./model-settings";
type Props = { client: ArielSocket; store: ExecutionSettingsStore; deviceId: string; agentEpoch: string; targetKey: string; threadId?: string; draft?: boolean; connected: boolean; ownerReady: boolean; queued: boolean; busy: boolean; hasImages: boolean; onState: (state: ExecutionGuard) => void };
export function ExecutionSettingsControl(p: Props) {
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
  useEffect(() => { callback.current({ key: p.targetKey, busy: saving || uncertain, imagesBlocked, ...(p.draft && draftValues ? { draftSettings: draftValues } : {}) }); }, [p.targetKey, p.draft, saving, uncertain, imagesBlocked, draftValues]);
  useEffect(() => {
    let cancelled = false; if (!p.connected || !p.deviceId) return;if(!catalog)setSupported(null);
    const key = `${p.client.sessionGeneration}\0${p.deviceId}\0${p.agentEpoch}`;
    void p.store.catalog(key, async () => {
      const response = await p.client.request("model.catalog.read", p.deviceId, reload ? { refresh: true } : {});
      if (response.error?.code === "PROTOCOL_UNSUPPORTED"||response.outcome === "accepted" && response.data?.settingsSupported !== true) throw new Error("unsupported");
      if (response.outcome !== "accepted" || response.data?.agentEpoch !== p.agentEpoch || !response.data.settingsSupported) throw new Error("unavailable");
      const result=parseCatalog(response.data.catalog);if(!result)throw new Error("unavailable");return result;
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
  if (supported !== true || !p.targetKey) return null;
  return <div className="execution-options">
    <button type="button" className="execution-trigger" ref={trigger} aria-label="模型与推理设置" aria-expanded={open} onClick={() => { setEditing(currentValues || (catalog && defaultValues(catalog)));setEditingRevision(snapshot?.revision||""); setAdjustment(""); setOpen(true); }}><span>{model?.name || currentValues?.model || "继承 Codex"}</span>{currentValues && <><span className="execution-separator">·</span><span>{effortLabel(currentValues.effort)}</span><span className="execution-separator">·</span><span>{speedLabel(currentValues.speed, model)}</span></>}<span aria-hidden="true">⌄</span>{saving && <span>保存中…</span>}</button>
    {imagesBlocked && <small role="status">当前模型不支持图片，请更换模型或移除截图。</small>}
    {open && createPortal(<div className="execution-backdrop" onClick={e => { if (e.target === e.currentTarget) close(); }}><div ref={dialog} role="dialog" aria-modal="true" aria-labelledby="execution-title" className="execution-dialog">
      <header><div><h3 id="execution-title">模型与推理设置</h3><p>仅影响当前会话的下一轮，正在执行的这一轮不变。</p></div><button type="button" aria-label="关闭模型设置" onClick={close} disabled={saving}>×</button></header>
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
