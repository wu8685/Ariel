import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { CircleButton, CloseGlyph } from "./circular-button";
import {
  FileViewer,
  emptyFileReaderState,
  type FileQuote,
  type FileReaderState,
  type FileView,
  type FileViewMode,
  type OpenFile,
} from "./file-context";
import { resolveDocumentPath } from "./document-path";
import type { AssetContext } from "./document-assets";
import "./file-workspace.css";

export type FileTarget = {
  deviceId: string;
  agentEpoch: string;
  threadId: string;
  turnId?: string;
  itemId?: string;
  path: string;
  source: "turn_change" | "message_reference" | "workspace_browser";
  kind?: "add" | "update" | "delete";
  referenceKind?: "markdown_link" | "plain_path";
  rawReference?: string;
  focusStartLine?: number;
  focusEndLine?: number;
};
type FileResource = { file?: OpenFile; diff?: string; diffTruncated?: boolean };
type FileTab = {
  id: string;
  key: string;
  target: FileTarget;
  mode: FileViewMode;
  hasDiff: boolean;
  reader: FileReaderState;
  loading: boolean;
  error: string;
  generation: number;
  diffChanged?: boolean;
};
export type FileWorkspaceState = {tabs:Pick<FileTab,"id"|"target"|"mode"|"hasDiff"|"reader">[];activeId:string;collapsed:boolean};
type Reply = {
  outcome: string;
  data?: Record<string, unknown>;
  error?: { code?: string };
};
type FileRequest = (
  method:
    | "file.context.read"
    | "file.context.reference.read"
    | "file.context.changes"
    | "workspace.file.read",
  deviceId: string,
  params: Record<string, unknown>,
) => Promise<Reply>;
const encoder = new TextEncoder();
export const fileTargetKey = (t: FileTarget) =>
  JSON.stringify([
    t.deviceId,
    t.agentEpoch,
    t.threadId,
    t.source,
    t.turnId || "",
    t.itemId || "",
    t.referenceKind || "",
    t.rawReference || t.path,
  ]);

// A local UX fingerprint, never an authorization receipt. No Diff text is persisted.
function diffFingerprint(text:string,truncated=false) {
  const lanes=[0x811c9dc5,0x9e3779b9,0x85ebca6b,0xc2b2ae35];
  for(let i=0;i<text.length;i++)for(let j=0;j<lanes.length;j++)lanes[j]=Math.imul(lanes[j]^text.charCodeAt(i),0x01000193+j*2);
  return lanes.map(n=>((n^(truncated?1:0))>>>0).toString(16).padStart(8,"0")).join("");
}

/** Logical tabs contain descriptors and reading anchors only; bodies share one LRU. */
export class FileWorkspaceModel {
  tabs: FileTab[] = [];
  activeId = "";
  collapsed = false;
  private serial = 0;
  private revision = 0;
  private listeners = new Set<() => void>();
  private readerListeners = new Set<() => void>();
  private resources = new Map<string, { value: FileResource; bytes: number; version:number }>();
  private cacheSerial=0;
  bytes = 0;
  constructor(
    private maxEntries = 10,
    private maxBytes = 8 * 1024 * 1024,
  ) {}
  get resourceCount() {
    return this.resources.size;
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  snapshot = () => this.revision;
  subscribeReader = (listener:()=>void) => {this.readerListeners.add(listener);return ()=>{this.readerListeners.delete(listener)}};
  readerChanged = (id:string) => {if(this.tabs.some(tab=>tab.id===id))for(const listener of this.readerListeners)listener()};
  private emit(persist=true) {
    this.revision++;
    for (const listener of this.listeners) listener();
    if(persist)for(const listener of this.readerListeners)listener();
  }
  restore(state:FileWorkspaceState) {
    this.clear();
    this.tabs=state.tabs.map(tab=>({...tab,target:{...tab.target},reader:{...tab.reader,positions:{...tab.reader.positions}},key:fileTargetKey(tab.target),loading:false,error:"",generation:0}));
    this.serial=Math.max(this.serial,...this.tabs.map(tab=>Number(tab.id.slice("file-tab-".length))));
    this.activeId=state.activeId;this.collapsed=state.collapsed;this.emit();
  }
  private resource(id: string) {
    const entry = this.resources.get(id);
    if (entry) {
      this.resources.delete(id);
      this.resources.set(id, entry);
    }
    return entry?.value;
  }
  private drop(id: string) {
    const previous = this.resources.get(id);
    if (previous) {
      this.bytes -= previous.bytes;
      this.resources.delete(id);
    }
  }
  private cache(id: string, value: FileResource) {
    const bytes =
      encoder.encode(value.file?.content || "").byteLength +
      encoder.encode(value.diff || "").byteLength;
    if (bytes > this.maxBytes)
      throw new Error("文件超过阅读缓存容量，请关闭部分内容后重试。");
    this.drop(id);
    this.resources.set(id, { value, bytes, version:++this.cacheSerial });
    this.bytes += bytes;
    while (
      this.resources.size > this.maxEntries ||
      this.bytes > this.maxBytes
    ) {
      // A late hidden read must not blank or reread the visible document.
      const first =
        [...this.resources.keys()].find((key) => key !== this.activeId) ??
        this.resources.keys().next().value;
      if (first === undefined) break;
      this.drop(first);
    }
  }
  open(
    target: FileTarget,
    mode: FileViewMode = "source",
    initial?: FileResource,
  ) {
    const key = fileTargetKey(target);
    let tab = this.tabs.find((t) => t.key === key);
    if (!tab) {
      tab = {
        id: `file-tab-${++this.serial}`,
        key,
        target: { ...target },
        mode,
        hasDiff: !!initial?.diff,
        reader: emptyFileReaderState(),
        loading: false,
        error: "",
        generation: 0,
      };
      this.tabs = [...this.tabs, tab];
    } else tab.mode = mode;
    this.activeId = tab.id;
    if (initial) {
      try {
        this.cache(tab.id, { ...this.resource(tab.id), ...initial });
        tab.hasDiff = !!initial.diff || tab.hasDiff;
        if(initial.diff!==undefined)this.verifyDiff(tab,initial.diff,!!initial.diffTruncated);
      } catch {
        tab.error = "文件超过阅读缓存容量。";
      }
    }
    this.collapsed = false;
    this.emit();
    return tab.id;
  }
  activate(id: string) {
    if (!this.tabs.some((t) => t.id === id)) return;
    this.activeId = id;
    this.collapsed = false;
    this.emit();
  }
  collapse() {
    this.collapsed = true;
    this.emit();
  }
  expand() {
    this.collapsed = false;
    this.emit();
  }
  close(id: string) {
    const index = this.tabs.findIndex((t) => t.id === id);
    if (index < 0) return;
    this.tabs[index].generation++;
    this.drop(id);
    this.tabs = this.tabs.filter((t) => t.id !== id);
    if (this.activeId === id)
      this.activeId =
        this.tabs[Math.min(index, this.tabs.length - 1)]?.id || "";
    this.emit();
  }
  clear() {
    for (const tab of this.tabs) tab.generation++;
    this.tabs = [];
    this.resources.clear();
    this.bytes = 0;
    this.activeId = "";
    this.collapsed = false;
    this.emit();
  }
  removeThread(deviceId: string, threadId: string) {
    const removed = this.tabs.filter(
      (tab) =>
        tab.target.deviceId === deviceId && tab.target.threadId === threadId,
    );
    if (!removed.length) return;
    for (const tab of removed) {
      tab.generation++;
      this.drop(tab.id);
    }
    this.tabs = this.tabs.filter((tab) => !removed.includes(tab));
    if (!this.tabs.some((tab) => tab.id === this.activeId))
      this.activeId = this.tabs[0]?.id || "";
    this.emit();
  }
  captureResources(){return new Map([...this.resources].map(([id,entry])=>[id,entry.version]))}
  invalidate(deviceId: string, threadId: string, path: string, fence?:ReadonlyMap<string,number>, completedSource?:{turnId:string;itemId:string}) {
    for (const tab of this.tabs) {
      if (
        tab.target.deviceId === deviceId &&
        tab.target.threadId === threadId &&
        tab.target.path === path
      ) {
        // Listing old completed-turn changes is not a new change notification.
        // Do not evict a current-workspace read obtained after that listing began.
        if(fence&&(!fence.has(tab.id)||fence.get(tab.id)!==this.resources.get(tab.id)?.version))continue;
        if(completedSource&&tab.target.source==="turn_change"&&tab.target.turnId===completedSource.turnId&&tab.target.itemId===completedSource.itemId)continue;
        tab.generation++;
        tab.loading = false;
        tab.error = "";
        this.drop(tab.id);
      }
    }
    this.emit();
  }
  mode(id: string, mode: FileViewMode) {
    const tab = this.tabs.find((t) => t.id === id);
    if (tab) {
      tab.mode = mode;
      tab.error = "";
      this.emit();
    }
  }
  view(id: string): FileView | undefined {
    const tab = this.tabs.find((t) => t.id === id);
    if (!tab) return;
    const r = this.resource(id),
      t = tab.target;
    return {
      threadId: t.threadId,
      turnId: t.turnId || "",
      change: {
        itemId: t.itemId || "",
        path: t.path,
        kind: t.kind || "update",
        diff: r?.diff,
        diffTruncated: r?.diffTruncated,
      },
      mode: tab.mode,
      file: r?.file,
      loading: tab.loading,
      diffChanged: tab.diffChanged,
      focusStartLine: t.focusStartLine,
      focusEndLine: t.focusEndLine,
    };
  }
  private verifyDiff(tab:FileTab,diff:string,truncated:boolean) {
    const fingerprint=diffFingerprint(diff,truncated);
    if(tab.reader.diffFingerprint!==fingerprint){
      tab.diffChanged=!!tab.reader.diffFingerprint;
      tab.reader.positions.diff=0;
    }
    tab.reader.diffFingerprint=fingerprint;
  }
  async load(id: string, request: FileRequest, retry = false) {
    const tab = this.tabs.find((t) => t.id === id);
    if (!tab || tab.loading) return;
    if (retry) {
      tab.error = "";
      this.drop(id);
    } else if (tab.error) return;
    const existing = this.resource(id);
    if (tab.mode === "source" ? existing?.file : existing?.diff !== undefined)
      return;
    const generation = ++tab.generation,
      t = { ...tab.target },
      mode = tab.mode;
    const current = () =>
      this.tabs.includes(tab) && tab.generation === generation;
    tab.loading = true;
    this.emit(false);
    try {
      let next: FileResource = { ...existing };
      if (mode === "diff") {
        const response = await request("file.context.changes", t.deviceId, {
          threadId: t.threadId,
          turnIds: [t.turnId],
        });
        const turns = response.data?.turns as
          { turnId: string; fileChanges: unknown[] }[] | undefined;
        const changes = Array.isArray(turns)
          ? turns.find((turn) => turn.turnId === t.turnId)?.fileChanges
          : undefined;
        const change = Array.isArray(changes)
          ? (changes.find(
              (value) =>
                value &&
                typeof value === "object" &&
                (value as { itemId: string }).itemId === t.itemId &&
                (value as { path: string }).path === t.path,
            ) as { diff?: unknown; diffTruncated?: boolean } | undefined)
          : undefined;
        if (response.outcome !== "accepted" || typeof change?.diff !== "string")
          throw new Error("无法读取此文件的 Diff。");
        next = {
          ...next,
          diff: change.diff,
          diffTruncated: change.diffTruncated,
        };
      } else {
        const method =
          t.source === "workspace_browser"
            ? "workspace.file.read"
            : t.source === "message_reference"
              ? "file.context.reference.read"
              : "file.context.read";
        const params = {
          threadId: t.threadId,
          ...(t.source === "workspace_browser"
            ? { path: t.path }
            : {
                turnId: t.turnId,
                itemId: t.itemId,
                ...(t.source === "message_reference"
                  ? {
                      referenceKind: t.referenceKind,
                      rawReference: t.rawReference,
                      ...(t.focusStartLine
                        ? {
                            startLine: t.focusStartLine,
                            endLine: t.focusEndLine || t.focusStartLine,
                          }
                        : {}),
                    }
                  : { path: t.path }),
              }),
        };
        const response = await request(method, t.deviceId, params),
          value = response.data;
        if (
          response.outcome !== "accepted" ||
          !value ||
          typeof value.content !== "string" ||
          typeof value.sha256 !== "string" ||
          typeof value.modifiedAtMs !== "number" ||
          typeof value.lineCount !== "number" ||
          typeof value.language !== "string" ||
          value.version !== "current-workspace" ||
          (t.source === "message_reference" &&
            typeof value.path !== "string") ||
          (t.source === "workspace_browser" && value.path !== t.path)
        )
          throw new Error("无法读取此文件，请重试。");
        const file: OpenFile = {
          source: t.source,
          threadId: t.threadId,
          ...(t.turnId ? { turnId: t.turnId } : {}),
          ...(t.itemId ? { itemId: t.itemId } : {}),
          path:
            t.source === "message_reference" ? (value.path as string) : t.path,
          content: value.content,
          sha256: value.sha256,
          modifiedAtMs: value.modifiedAtMs,
          lineCount: value.lineCount,
          language: value.language,
          version: "current-workspace",
          documentAssets: value.documentAssets as OpenFile["documentAssets"],
          ...(t.referenceKind && t.rawReference
            ? { referenceKind: t.referenceKind, rawReference: t.rawReference }
            : {}),
        };
        next = { ...next, file };
      }
      if (!current()) return;
      if(next.diff!==undefined)this.verifyDiff(tab,next.diff,!!next.diffTruncated);
      this.cache(id, next);
      if (next.file) tab.target.path = next.file.path;
    } catch (error) {
      if (current())
        tab.error = error instanceof Error ? error.message : "无法读取此文件。";
    } finally {
      if (current()) {
        tab.loading = false;
        this.emit();
      }
    }
  }
}

export function useFileWorkspace(model: FileWorkspaceModel) {
  return useSyncExternalStore(model.subscribe, model.snapshot, model.snapshot);
}

export function FileWorkspace({
  model,
  request,
  currentThreadId,
  onQuote,
  assetContext,
  onBrowse,
  canReadWorkspace = true,
  canLoad = true,
  persistenceWarning = "",
  onReturnFocus,
  suspended = false,
}: {
  model: FileWorkspaceModel;
  request: FileRequest;
  currentThreadId: string;
  onQuote: (quote: FileQuote) => void;
  assetContext?: AssetContext;
  onBrowse?: () => void;
  canReadWorkspace?: boolean;
  canLoad?: boolean;
  persistenceWarning?: string;
  onReturnFocus?: () => void;
  suspended?: boolean;
}) {
  const revision = useFileWorkspace(model);
  const [all, setAll] = useState(false),
    [search, setSearch] = useState(""),
    [page, setPage] = useState(0);
  const [controlsHost, setControlsHost] = useState<HTMLDivElement|null>(null);
  const menuId = useId();
  const menuRef = useRef<HTMLDivElement>(null);
  const menuTrigger = useRef<HTMLButtonElement|null>(null);
  function closeMenu(restoreFocus=false) {
    setAll(false);
    if (restoreFocus) menuTrigger.current?.focus({preventScroll:true});
  }
  const active = model.tabs.find((tab) => tab.id === model.activeId),
    view = active ? model.view(active.id) : undefined;
  const tabRefs = useRef(new Map<string, HTMLButtonElement>());
  const restoreButton = useRef<HTMLButtonElement>(null);
  const focusPending = useRef(false);
  const closeTab = (id: string) => {
    focusPending.current = true;
    model.close(id);
  };
  const collapse = () => {
    focusPending.current = true;
    model.collapse();
  };
  useEffect(() => {
    if (!focusPending.current) return;
    focusPending.current = false;
    if (model.collapsed || !model.tabs.length) {
      if (onReturnFocus) onReturnFocus();
      else restoreButton.current?.focus();
    } else tabRefs.current.get(model.activeId)?.focus();
  }, [revision, onReturnFocus]);
  useEffect(() => {
    if (active && !model.collapsed && canLoad) void model.load(active.id, request);
  }, [revision, request, canLoad]);
  useEffect(() => {
    if (model.collapsed) return;
    const tab = tabRefs.current.get(model.activeId);
    tab?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
    tab?.focus({ preventScroll: true });
  }, [model.activeId, model.collapsed]);
  useEffect(() => {
    if (model.collapsed) setAll(false);
  }, [model.collapsed]);
  useEffect(() => { setAll(false); }, [model.activeId]);
  useEffect(() => {
    if (!all) return;
    const outside = (event:PointerEvent) => {
      if (event.target instanceof Node && !menuRef.current?.contains(event.target) && !menuTrigger.current?.contains(event.target)) closeMenu();
    };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [all]);
  if (!active || !view) return null;
  if (model.collapsed)
    return (
      <div className="file-workspace-restore">
        {persistenceWarning&&<p className="file-session-warning" role="status">{persistenceWarning}</p>}
        <button
          ref={restoreButton}
          type="button"
          aria-label={`展开文件工作区（${model.tabs.length} 个文件）`}
          onClick={() => model.expand()}
        >
          文件 <span>{model.tabs.length}</span> ·{" "}
          {active.target.path.split("/").at(-1)}{" "}
          <span aria-hidden="true">↗</span>
        </button>
      </div>
    );
  const filtered = model.tabs.filter((tab) =>
    tab.target.path.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  const visible = filtered.slice(page * 40, page * 40 + 40);
  return (
    <section
      className="file-workspace"
      role="region"
      aria-label="文件工作区"
      aria-hidden={suspended || undefined}
      inert={suspended}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          if (all) closeMenu(true);
          else collapse();
        }
      }}
    >
      <div className="file-workspace-toolbar">
        <div
          className="file-workspace-tabs"
          role="tablist"
          aria-label="文件标签"
        >
          {model.tabs.map((tab, index) => (
            <div
              className={
                "file-workspace-tab" +
                (tab.id === model.activeId ? " active" : "")
              }
              key={tab.id}
            >
              <button
                type="button"
                role="tab"
                id={tab.id}
                aria-controls="file-workspace-body"
                aria-selected={tab.id === model.activeId}
                tabIndex={tab.id === model.activeId ? 0 : -1}
                aria-label={tab.target.path}
                title={tab.target.path}
                ref={(node) => {
                  if (node) tabRefs.current.set(tab.id, node);
                  else tabRefs.current.delete(tab.id);
                }}
                onClick={() => model.activate(tab.id)}
                onKeyDown={(event) => {
                  let next = index;
                  if (event.key === "ArrowRight")
                    next = (index + 1) % model.tabs.length;
                  else if (event.key === "ArrowLeft")
                    next = (index + model.tabs.length - 1) % model.tabs.length;
                  else if (event.key === "Home") next = 0;
                  else if (event.key === "End") next = model.tabs.length - 1;
                  else if (event.key === "Delete") {
                    event.preventDefault();
                    closeTab(tab.id);
                    return;
                  } else return;
                  event.preventDefault();
                  model.activate(model.tabs[next].id);
                  tabRefs.current.get(model.tabs[next].id)?.focus();
                }}
              >
                <span aria-hidden="true">▤</span>
                <span>{tab.target.path.split("/").at(-1)}</span>
              </button>
              <button
                className="file-tab-close"
                type="button"
                aria-label={`关闭标签 ${tab.target.path}`}
                onClick={() => closeTab(tab.id)}
              >
                <CloseGlyph />
              </button>
            </div>
          ))}
        </div>
        <CircleButton
          type="button"
          aria-label="文件菜单"
          aria-expanded={all}
          aria-controls={menuId}
          onClick={(event) => {
            menuTrigger.current = event.currentTarget;
            setAll((value) => !value);
            setPage(0);
          }}
        >
          ⌄
        </CircleButton>
        {onBrowse && (
          <CircleButton
            type="button"
            aria-label="打开其他文件"
            onClick={onBrowse}
          >
            ＋
          </CircleButton>
        )}
        <CircleButton
          type="button"
          aria-label="收起文件工作区"
          onClick={collapse}
        >
          −
        </CircleButton>
      </div>
      {all && (
        <div className="file-workspace-all" id={menuId} ref={menuRef}>
          {!active.error && <>
            <span className="file-menu-heading">当前文件视图</span>
            <div className="file-menu-controls" ref={setControlsHost}/>
          </>}
          <span className="file-menu-heading">已打开的文件</span>
          <input
            type="search"
            aria-label="搜索文件标签"
            placeholder="搜索已打开文件"
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(0);
            }}
          />
          <div role="listbox" aria-label="全部已打开文件">
            {visible.map((tab) => (
              <button
                type="button"
                role="option"
                aria-selected={tab.id === model.activeId}
                key={tab.id}
                onClick={() => {
                  focusPending.current = true;
                  model.activate(tab.id);
                  setAll(false);
                }}
              >
                {tab.target.path}
              </button>
            ))}
          </div>
          <div className="file-tab-pages">
            <button
              type="button"
              disabled={page === 0}
              onClick={() => setPage((value) => value - 1)}
            >
              上一页
            </button>
            <span>{filtered.length} 个文件</span>
            <button
              type="button"
              disabled={(page + 1) * 40 >= filtered.length}
              onClick={() => setPage((value) => value + 1)}
            >
              下一页
            </button>
          </div>
        </div>
      )}
      {persistenceWarning&&<p className="file-session-warning" role="status">{persistenceWarning}</p>}
      <div
        className="file-workspace-body"
        id="file-workspace-body"
        role="tabpanel"
        aria-labelledby={active.id}
      >
        {!canLoad ? <div className="file-workspace-error" role="status">正在等待连接或设备就绪；可先收起文件，恢复连接后重试。</div> : active.error ? (
          <div className="file-workspace-error">
            <p role="alert">{active.error}</p>
            <button
              type="button"
              aria-label="重试读取文件"
              onClick={() => void model.load(active.id, request, true)}
            >
              重试
            </button>
          </div>
        ) : active.loading && view.mode === "diff" ? (
          <p role="status">正在读取 Diff…</p>
        ) : (
          <FileViewer
            key={active.id}
            view={view}
            readerState={active.reader}
            onReaderChange={() => model.readerChanged(active.id)}
            controlsHost={controlsHost}
            onViewControl={() => closeMenu(true)}
            embedded
            assetContext={assetContext}
            source={active.target.source}
            showDiff={active.target.source === "turn_change"}
            diffAvailable={active.hasDiff}
            quoteDisabled={currentThreadId !== active.target.threadId}
            onClose={collapse}
            onModeChange={(mode) => model.mode(active.id, mode)}
            onQuote={(quote) => {
              if (quote.threadId === currentThreadId) onQuote(quote);
            }}
            onOpenDocument={
              canReadWorkspace
                ? (target) => {
                    const resolved = resolveDocumentPath(
                      active.target.path,
                      target,
                    );
                    if (!resolved) throw new Error("文档路径不允许读取");
                    model.open({
                      deviceId: active.target.deviceId,
                      agentEpoch: active.target.agentEpoch,
                      threadId: active.target.threadId,
                      path: resolved.path,
                      source: "workspace_browser",
                      focusStartLine: resolved.startLine,
                    });
                  }
                : undefined
            }
          />
        )}
      </div>
    </section>
  );
}
