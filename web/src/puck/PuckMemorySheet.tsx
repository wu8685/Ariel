import { useEffect, useRef, useState } from "react";

type Request = (method: string, target: string, params: Record<string, unknown>) => Promise<{ outcome: string; data?: Record<string, unknown>; error?: { message?: string } }>;
type MemoryEntry = { id: string; scope: string; kind: string; text: string; writerName?: string; createdAt?: string };

export function PuckMemorySheet({ assistantId, request, onClose }: { assistantId: string; request: Request; onClose: () => void }) {
  const [scope, setScope] = useState<"bot" | "shared">("bot");
  const [entries, setEntries] = useState<MemoryEntry[]>([]);
  const [cursor, setCursor] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [pending, setPending] = useState<MemoryEntry | undefined>();
  const requestRef = useRef(request);
  requestRef.current = request;
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    void requestRef.current("puck.memory.list", "relay", { assistantId, scope, kind: "any", limit: 50 }).then(response => {
      if (cancelled) return;
      setLoading(false);
      if (response.outcome !== "accepted") {
        setError(response.error?.message || "记忆暂时读不了");
        setEntries([]);
        return;
      }
      const list = Array.isArray(response.data?.entries) ? response.data?.entries as MemoryEntry[] : [];
      setEntries(list);
      setCursor(typeof response.data?.nextCursor === "string" ? response.data.nextCursor : "");
      setError("");
    });
    return () => { cancelled = true; };
  }, [assistantId, scope]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      if (pending) setPending(undefined);
      else onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, pending]);
  async function remove(entry: MemoryEntry) {
    const response = await request("puck.memory.delete", "relay", { assistantId, memoryId: entry.id });
    if (response.outcome !== "accepted") {
      setError(response.error?.message || "没有删掉");
      return;
    }
    setEntries(current => current.filter(item => item.id !== entry.id));
    setPending(undefined);
  }
  const profiles = entries.filter(entry => entry.kind === "profile");
  const journals = entries.filter(entry => entry.kind === "journal");
  return <div className="puck-memory-sheet-backdrop" role="presentation" onClick={onClose}>
    <section className="puck-memory-sheet" role="dialog" aria-modal="true" aria-labelledby="puck-memory-title" onClick={event => event.stopPropagation()}>
      <header className="puck-memory-sheet-header">
        <button className="puck-memory-sheet-back" type="button" aria-label="返回" onClick={onClose}>×</button>
        <h2 id="puck-memory-title">记忆</h2>
      </header>
      <div className="puck-memory-tabs" role="tablist" aria-label="记忆范围">
        <button type="button" role="tab" aria-selected={scope === "bot"} onClick={() => setScope("bot")}>这个 Bot</button>
        <button type="button" role="tab" aria-selected={scope === "shared"} onClick={() => setScope("shared")}>共享</button>
      </div>
      {error && <p className="puck-memory-error" role="alert">{error}</p>}
      {loading ? <p className="puck-memory-empty">正在读取…</p> : entries.length === 0 ? <p className="puck-memory-empty">还没有记忆。</p> : <div className="puck-memory-list">
        <MemoryGroup title="长期资料" entries={profiles} shared={scope === "shared"} onDelete={setPending}/>
        <MemoryGroup title="最近记录" entries={journals} shared={scope === "shared"} onDelete={setPending}/>
      </div>}
      {cursor && <button className="puck-btn" type="button" onClick={() => void request("puck.memory.list", "relay", { assistantId, scope, kind: "any", cursor, limit: 50 }).then(response => {
        const more = Array.isArray(response.data?.entries) ? response.data?.entries as MemoryEntry[] : [];
        setEntries(current => [...current, ...more]);
        setCursor(typeof response.data?.nextCursor === "string" ? response.data.nextCursor : "");
      })}>更多</button>}
      {pending && <div className="puck-dialog-backdrop" role="presentation"><section className="puck-dialog" role="dialog" aria-modal="true" aria-labelledby="puck-memory-delete-title"><h2 id="puck-memory-delete-title">删除这条记忆？</h2><p>{pending.scope === "shared" ? "这是共享记忆，删除后所有 Bot 都不再看到。" : "这个 Bot 之后的回合不会再看到它。"}</p><div><button className="puck-btn puck-btn-md" type="button" onClick={() => setPending(undefined)}>取消</button><button className="puck-btn puck-btn-md danger" type="button" aria-label={`确认删除记忆 ${pending.text}`} onClick={() => void remove(pending)}>删除</button></div></section></div>}
    </section>
  </div>;
}

function MemoryGroup({ title, entries, shared, onDelete }: { title: string; entries: MemoryEntry[]; shared: boolean; onDelete: (entry: MemoryEntry) => void }) {
  if (entries.length === 0) return null;
  return <section><h3>{title}</h3><ul>{entries.map(entry => <li key={entry.id}><p>{entry.text}</p><small>{entry.createdAt ? new Date(entry.createdAt).toLocaleDateString("zh-CN", { timeZone: "Asia/Shanghai" }) : ""}{shared && entry.writerName ? ` · ${entry.writerName}` : ""}</small><button className="puck-btn" type="button" aria-label={`删除记忆 ${entry.text}`} onClick={() => onDelete(entry)}>删除</button></li>)}</ul></section>;
}
