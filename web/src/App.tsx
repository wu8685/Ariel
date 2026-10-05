import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArielSocket, isWebPIN, type ConnectionStatus } from "./client";
import { applyThreadEvent, belongsToSubscription, keepOfflineDevice, preserveDraftAfterSend, recoveryTarget, permissionSummary, canSend, type ThreadView } from "./state";
import { answersForSubmission } from "./interaction";
import { newRequestID } from "./ids";
import { WebSession } from "./session";
import { appendOlderPage, emptyHistoryState, prependOlderItems, type HistoryState } from "./history";
import { activityStatusText, groupTurnItems } from "./activity";
import { ConversationMarkdown } from "./markdown";
import type { ArielProtocolV1Envelope, Thread, Turn, Item, Response, Interaction } from "./generated/protocol";
import "./interaction.css";

type Device = { deviceId: string; deviceName: string; agentOnline: boolean; codexReady: boolean; agentEpoch?: string; adapterVersion?: string; capabilities: { autoLoad: boolean; history?: boolean; send?: boolean; interrupt?: boolean; interaction?: boolean } };
const wsURL = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws`;
const recentTurnLimit = 10;
const mobileViewportMaxWidth = 800;
const mobileComposerMinHeight = 44;
const mobileComposerMaxHeight = 24 * 8 + 20; // Eight 24px lines plus vertical padding.
const latestFollowDistance = 80;
type ReadingAnchor = { key: string; itemId: string; top: number; scrollTop: number; scrollHeight: number };

function readingKey(deviceId: string, threadId: string): string { return `${deviceId}\u0000${threadId}`; }

function captureReadingAnchor(container: HTMLElement | null, key: string): ReadingAnchor | null {
  if (!container) return null;
  const top = container.getBoundingClientRect().top;
  const messages = [...container.querySelectorAll<HTMLElement>("[data-item-id]")];
  const visible = messages.find(message => message.getBoundingClientRect().bottom > top + 1) || messages.at(-1);
  if (!visible?.dataset.itemId) return null;
  return { key, itemId: visible.dataset.itemId, top: visible.getBoundingClientRect().top, scrollTop: container.scrollTop, scrollHeight: container.scrollHeight };
}

function restoreReadingAnchor(container: HTMLElement, anchor: ReadingAnchor): boolean {
  const target = [...container.querySelectorAll<HTMLElement>("[data-item-id]")].find(message => message.dataset.itemId === anchor.itemId);
  if (!target) return false;
  container.scrollTop = anchor.scrollTop + target.getBoundingClientRect().top - anchor.top;
  return true;
}

function scrollToLatest(container: HTMLElement) {
  container.scrollTop = Math.max(0, container.scrollHeight - container.clientHeight);
}

type ContentMarker = { turnCount: number; turnId: string; itemCount: number; itemId: string; text: string; details: string; cardCount: number; cardId: string };
function contentMarker(thread: Thread): ContentMarker {
  const lastTurn = thread.turns.at(-1);
  const lastItem = lastTurn?.items.at(-1);
  return {
    turnCount: thread.turns.length, turnId: lastTurn?.turnId || "", itemCount: lastTurn?.items.length || 0,
    itemId: lastItem?.itemId || "", text: lastItem?.text || "", details: lastItem?.activity?.details || "",
    cardCount: thread.pendingInteractions.length, cardId: thread.pendingInteractions.at(-1)?.interactionId || "",
  };
}
function hasNewVisibleContent(previous: ContentMarker | null, current: ContentMarker): boolean {
  return !!previous && (Object.keys(current) as (keyof ContentMarker)[]).some(key => previous[key] !== current[key]);
}
const resultText: Record<string, string> = { DEVICE_OFFLINE: "设备离线，请确认电脑上的 Agent 已连接。", TURN_BUSY: "这个会话正在运行；草稿已保留，不会自动重发。", STALE_TURN: "运行中的 turn 已变化，请刷新状态后再停止。", STALE_INTERACTION: "这项交互已经变化或过期，请查看最新会话状态。", OUTCOME_UNKNOWN: "执行结果不确定。请先查看会话状态，不要直接重发。", RESYNC_REQUIRED: "事件顺序发生变化，正在重新同步。", NATIVE_STATE_UNCERTAIN: "Codex 原生会话状态暂时无法确认，已停止此会话的远程操作。请稍后手动重新选择；若持续出现，请在电脑端查看。", HISTORY_TOO_LARGE: "此页内容超过安全传输上限；已保留当前可见内容。", INTERACTION_UNSUPPORTED: "这张卡片已失效或当前决定不可用。", INVALID_ARGUMENT: "请求内容无效。", OVERLOADED: "请求过多，请稍后再试。", PROTOCOL_UNSUPPORTED: "当前 Codex 版本不支持历史分页。" };

function errorText(response: Response): string {
  return response.error ? resultText[response.error.code] || response.error.message : "操作未完成。";
}

function BrandMark() {
  return <svg className="brand-mark" aria-hidden="true" viewBox="0 0 32 32" focusable="false">
    <path d="M16 2v28 M2 16h28 M6.1 6.1l19.8 19.8 M25.9 6.1 6.1 25.9" />
  </svg>;
}

export function App() {
  const client = useMemo(() => new ArielSocket(wsURL), []);
  const webSession = useMemo(() => new WebSession(() => window.sessionStorage), []);
  const [token, setToken] = useState("");
  const [sessionExpired, setSessionExpired] = useState(false);
  const savedSessionAttempt = useRef(false);
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const [devices, setDevices] = useState<Device[]>([]);
  const [deviceId, setDeviceId] = useState("");
  const [threads, setThreads] = useState<Thread[]>([]);
  const [cursor, setCursor] = useState("");
  const [threadId, setThreadId] = useState("");
  const [view, setView] = useState<ThreadView | null>(null);
  const [readOnlyHistory, setReadOnlyHistory] = useState(false);
  const [history, setHistory] = useState<HistoryState>(emptyHistoryState);
  const historyRef = useRef<HistoryState>(emptyHistoryState());
  const [historyLoading, setHistoryLoading] = useState(false);
  const historyLoadingRef = useRef(false);
  const [itemLoading, setItemLoading] = useState("");
  const itemLoadingRef = useRef("");
  const [itemOverrides, setItemOverrides] = useState<Record<string, Turn>>({});
  const [draft, setDraft] = useState("");
  const [notice, setNotice] = useState("");
  const [working, setWorking] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [showList, setShowList] = useState(true);
  const [showReturnToLatest, setShowReturnToLatest] = useState(false);
  const [permissionInfoOpen, setPermissionInfoOpen] = useState(false);
  const [visualViewportHeight, setVisualViewportHeight] = useState<number | null>(null);
  const [answers, setAnswers] = useState<Record<string, Record<string, string>>>({});
  const selection = useRef({ deviceId: "", threadId: "", view: null as ThreadView | null });
  const expectedSubscription = useRef("");
  const blockedSelection = useRef("");
  const pendingSelect = useRef(0);
  const deviceListGeneration = useRef(0);
  const threadListGeneration = useRef(0);
  const resuming = useRef(false);
  const transcriptRef = useRef<HTMLDivElement>(null);
  const followLatestRef = useRef(true);
  const pendingReadingAnchor = useRef<ReadingAnchor | null>(null);
  const resumeReadingAnchor = useRef<ReadingAnchor | null>(null);
  const lastPaint = useRef<{ key: string; seq: number; marker: ContentMarker | null }>({ key: "", seq: -1, marker: null });
  const permissionInfoRef = useRef<HTMLDivElement>(null);
  const composerInputRef = useRef<HTMLTextAreaElement>(null);
  const device = devices.find(d => d.deviceId === deviceId);
  const selectedThread = threads.find(t => t.threadId === threadId);
  const mock = device?.adapterVersion?.startsWith("mock-") ?? false;
  const activeTurn = view?.thread.turns.findLast(t => t.status === "inProgress");
  const currentPermissions = view && !mock ? permissionSummary(view.thread.permissions) : null;
  const connectionLabel = status === "ready" ? "Relay 已连接" : status === "connecting" ? "正在连接" : status === "invalid" ? "连接未通过" : "Relay 未连接";
  const permissionWarning = Boolean(currentPermissions?.warning || currentPermissions?.label.includes("未知"));
  const displayedTurns = [...history.older, ...(view?.thread.turns || [])].map(turn => {
    const override = itemOverrides[turn.turnId];
    if (!override || turn.itemsComplete !== false) return turn;
    const known = new Set(override.items.map(item => item.itemId));
    return { ...turn, items: [...override.items, ...turn.items.filter(item => !known.has(item.itemId))], itemsComplete: override.itemsComplete, nextItemCursor: override.nextItemCursor };
  });

  function resetHistory() {
    const empty = emptyHistoryState();
    historyRef.current = empty;
    historyLoadingRef.current = false;
    setHistory(empty);
    setHistoryLoading(false);
    setItemLoading("");
    itemLoadingRef.current = "";
    setItemOverrides({});
  }

  function rememberReadingPosition() {
    if (!selection.current.deviceId || !selection.current.threadId || followLatestRef.current) return;
    const anchor = captureReadingAnchor(transcriptRef.current, readingKey(selection.current.deviceId, selection.current.threadId));
    if (anchor) resumeReadingAnchor.current = anchor;
  }

  function onTranscriptScroll() {
    const container = transcriptRef.current;
    if (!container) return;
    followLatestRef.current = container.scrollHeight - container.clientHeight - container.scrollTop <= latestFollowDistance;
    if (followLatestRef.current) setShowReturnToLatest(false);
  }

  function returnToLatest() {
    followLatestRef.current = true;
    setShowReturnToLatest(false);
    const container = transcriptRef.current;
    if (container) scrollToLatest(container);
  }

  function disconnect() {
    webSession.disconnect(); savedSessionAttempt.current = false; setSessionExpired(false); setToken(""); client.disconnect();
  }

  async function refreshDevices(): Promise<Device[]> {
    const generation = ++deviceListGeneration.current;
    const response = await client.request("device.list", "relay", {});
    if (generation !== deviceListGeneration.current) return [];
    if (response.outcome !== "accepted") { setNotice(errorText(response)); return []; }
    const found = (response.data?.devices as Device[] | undefined) || [];
    setDevices(previous => keepOfflineDevice(found, previous, selection.current.deviceId));
    setDeviceId(current => current || found[0]?.deviceId || "");
    return found;
  }

  async function loadThreads(id: string, next = "") {
    const generation = ++threadListGeneration.current;
    if (!id) { setThreads([]); return; }
    const response = await client.request("thread.list", id, { limit: 50, ...(next ? { cursor: next } : {}) });
    if (generation !== threadListGeneration.current || selection.current.deviceId !== id) return;
    if (response.outcome !== "accepted") { setNotice(errorText(response)); return; }
    const page = (response.data?.threads as Thread[] | undefined) || [];
    setThreads(current => next ? [...current, ...page] : page);
    setCursor(String(response.data?.nextCursor || ""));
  }

  async function showReadOnlyHistory(id: string, targetDevice: string, epoch: number): Promise<boolean> {
    const stored = await client.request("thread.read", targetDevice, { threadId: id });
    if (epoch !== pendingSelect.current || selection.current.threadId !== id || selection.current.deviceId !== targetDevice) return false;
    const thread = stored.data?.thread as Thread | undefined;
    if (stored.outcome !== "accepted" || thread?.threadId !== id) {
      setNotice(stored.outcome === "accepted" ? "历史页身份无法确认。" : errorText(stored));
      return false;
    }
    const readOnlyThread: Thread = { ...thread, runtime: "unknown", pendingInteractions: [], permissions: { sandbox: "unknown", approval: "unknown" } };
    const readOnlyView: ThreadView = { deviceId: targetDevice, threadId: id, subscriptionId: "", streamId: "", seq: 0, thread: readOnlyThread };
    selection.current.view = readOnlyView;
    setView(readOnlyView);
    setReadOnlyHistory(true);
    setNotice("");
    return true;
  }

  async function selectThread(id: string, targetDevice = deviceId) {
    if (!targetDevice) return;
    if (selection.current.deviceId === targetDevice && selection.current.threadId === id) rememberReadingPosition();
    else { resumeReadingAnchor.current = null; followLatestRef.current = true; setShowReturnToLatest(false); }
    pendingReadingAnchor.current = null;
    blockedSelection.current = "";
    const epoch = ++pendingSelect.current;
    const old = selection.current.view;
    const oldDevice = selection.current.deviceId;
    const oldSubscription = expectedSubscription.current;
    expectedSubscription.current = "";
    selection.current = { deviceId: targetDevice, threadId: id, view: null };
    setThreadId(id); setView(null); setReadOnlyHistory(false); setNotice(""); setShowList(false); resetHistory();
    if (oldSubscription) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    else if (old) void client.request("thread.unsubscribe", old.deviceId, { subscriptionId: old.subscriptionId });
    const response = await client.request("thread.subscribe", targetDevice, { threadId: id });
    if (epoch !== pendingSelect.current) {
      const staleID = response.data?.subscriptionId;
      if (response.outcome === "accepted" && typeof staleID === "string") void client.request("thread.unsubscribe", targetDevice, { subscriptionId: staleID });
      return;
    }
    if (response.outcome !== "accepted") {
      if (response.error?.code === "HISTORY_TOO_LARGE" || response.error?.code === "OVERLOADED") {
        setReadOnlyHistory(true);
        if (await showReadOnlyHistory(id, targetDevice, epoch)) return;
        blockedSelection.current = `${targetDevice}\u0000${id}`;
        setNotice("原 owner 状态无法确认，历史分页也未能加载。可稍后手动重新选择此会话。");
        return;
      }
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
    await selectThread(current.threadId, current.deviceId);
  }

  useEffect(() => {
    client.onStatus = next => {
      setStatus(next);
      if (next === "invalid") {
        webSession.rejected();
        if (savedSessionAttempt.current) setSessionExpired(true);
        savedSessionAttempt.current = false;
      }
      if (next !== "ready") { rememberReadingPosition(); pendingSelect.current++; deviceListGeneration.current++; threadListGeneration.current++; expectedSubscription.current = ""; selection.current.view = null; setView(null); setReadOnlyHistory(false); resetHistory(); }
    };
    client.onReady = (_epoch, sessionToken) => {
      if (sessionToken) webSession.accepted(sessionToken);
      savedSessionAttempt.current = Boolean(sessionToken);
      setSessionExpired(false);
      expectedSubscription.current = ""; selection.current.view = null; setView(null); setReadOnlyHistory(false); resetHistory();
      void refreshDevices().then(online => void resumeSelected(online));
    };
    client.onEvent = (event: ArielProtocolV1Envelope) => {
      if (event.type !== "event") return;
      if (event.event === "device.status") {
        if (!event.agentOnline && event.deviceId === selection.current.deviceId) {
          rememberReadingPosition();
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
          if (event.code === "HISTORY_TOO_LARGE") {
            expectedSubscription.current = "";
            setReadOnlyHistory(true);
            void showReadOnlyHistory(current.threadId, current.deviceId, pendingSelect.current);
            return;
          }
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
        if (current.view?.deviceId === event.deviceId && current.view.threadId === event.threadId) rememberReadingPosition();
        const next = applyThreadEvent(null, event);
        resetHistory(); selection.current.view = next; setView(next); setReadOnlyHistory(false); setNotice("");
      } else if (event.event === "thread.update") {
        const next = applyThreadEvent(current.view, event);
        if (!next) { void resync(); return; }
        selection.current.view = next; setView(next);
        setThreads(list => list.map(t => t.threadId === next.threadId ? next.thread : t));
      }
    };
    const saved = webSession.saved();
    if (saved) { savedSessionAttempt.current = true; client.connect(saved); }
    return () => client.disconnect();
  }, [client]);

  useEffect(() => {
    pendingSelect.current++;
    threadListGeneration.current++;
    const oldDevice = selection.current.deviceId;
    if (oldDevice && oldDevice !== deviceId) { resumeReadingAnchor.current = null; followLatestRef.current = true; setShowReturnToLatest(false); }
    const oldSubscription = expectedSubscription.current;
    if (oldDevice && oldSubscription) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    expectedSubscription.current = "";
    selection.current = { deviceId, threadId: "", view: null };
    setThreadId(""); setView(null); setReadOnlyHistory(false); setThreads([]); setCursor(""); resetHistory();
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

  useEffect(() => {
    if (!showList) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setShowList(false);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [showList]);

  useEffect(() => {
    if (!permissionInfoOpen) return;
    const onPointerDown = (event: MouseEvent) => {
      if (!permissionInfoRef.current?.contains(event.target as Node)) setPermissionInfoOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setPermissionInfoOpen(false);
    };
    document.addEventListener("mousedown", onPointerDown);
    window.addEventListener("keydown", onKeyDown);
    return () => { document.removeEventListener("mousedown", onPointerDown); window.removeEventListener("keydown", onKeyDown); };
  }, [permissionInfoOpen]);

  useEffect(() => { setPermissionInfoOpen(false); }, [deviceId, threadId]);

  useEffect(() => {
    const input = composerInputRef.current;
    if (!input) return;
    const resize = () => {
      const previousScrollTop = input.scrollTop;
      const caretAtEnd = input.selectionEnd === input.value.length;
      input.style.height = "";
      if (window.innerWidth > mobileViewportMaxWidth) return;
      input.style.height = "auto";
      const contentHeight = input.scrollHeight;
      input.style.height = `${Math.max(mobileComposerMinHeight, Math.min(contentHeight, mobileComposerMaxHeight))}px`;
      input.scrollTop = contentHeight > mobileComposerMaxHeight ? (caretAtEnd ? contentHeight : previousScrollTop) : 0;
    };
    resize();
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, [draft, view]);

  useEffect(() => {
    const viewport = window.visualViewport;
    const input = composerInputRef.current;
    if (!viewport || !input) return;
    const sync = () => {
      const visibleBottom = viewport.offsetTop + viewport.height;
      const keyboardVisible = window.innerWidth <= 800 && document.activeElement === input && visibleBottom < window.innerHeight - 80;
      setVisualViewportHeight(keyboardVisible ? Math.round(visibleBottom) : null);
    };
    const onBlur = () => setVisualViewportHeight(null);
    viewport.addEventListener("resize", sync);
    viewport.addEventListener("scroll", sync);
    window.addEventListener("resize", sync);
    input.addEventListener("focus", sync);
    input.addEventListener("blur", onBlur);
    return () => {
      viewport.removeEventListener("resize", sync);
      viewport.removeEventListener("scroll", sync);
      window.removeEventListener("resize", sync);
      input.removeEventListener("focus", sync);
      input.removeEventListener("blur", onBlur);
    };
  }, []);

  useLayoutEffect(() => {
    const container = transcriptRef.current;
    if (!container || !view) return;
    const key = readingKey(view.deviceId, view.threadId);
    const marker = contentMarker(view.thread);
    const pending = pendingReadingAnchor.current;
    if (pending?.key === key) {
      if (!restoreReadingAnchor(container, pending)) container.scrollTop = pending.scrollTop + container.scrollHeight - pending.scrollHeight;
      pendingReadingAnchor.current = null;
      lastPaint.current = { key, seq: view.seq, marker };
      return;
    }
    const resume = resumeReadingAnchor.current;
    if (resume?.key === key) {
      if (restoreReadingAnchor(container, resume)) followLatestRef.current = false;
      else { followLatestRef.current = true; scrollToLatest(container); setNotice("原阅读位置已不在当前历史窗口，已回到最新消息。"); }
      resumeReadingAnchor.current = null;
      lastPaint.current = { key, seq: view.seq, marker };
      return;
    }
    if (lastPaint.current.key !== key) {
      followLatestRef.current = true;
      setShowReturnToLatest(false);
      scrollToLatest(container);
    } else if (lastPaint.current.seq !== view.seq) {
      if (followLatestRef.current) scrollToLatest(container);
      else if (hasNewVisibleContent(lastPaint.current.marker, marker)) setShowReturnToLatest(true);
    } else if (followLatestRef.current) scrollToLatest(container);
    lastPaint.current = { key, seq: view.seq, marker };
  }, [view?.seq, view?.threadId, history, itemOverrides]);

  async function loadOlder(additionalTurns = 1) {
    if (!view || !deviceId || historyLoadingRef.current || historyRef.current.exhausted) return;
    const epoch = pendingSelect.current;
    const targetThread = view.threadId;
    const targetCount = historyRef.current.older.length + additionalTurns;
    historyLoadingRef.current = true;
    setHistoryLoading(true);
    try {
      for (let pageNumber = 0; pageNumber < 20; pageNumber++) {
        const current = historyRef.current;
        const response = await client.request("thread.history", deviceId, { threadId: targetThread, limit: 10, ...(current.cursor ? { cursor: current.cursor } : {}) });
        if (epoch !== pendingSelect.current || selection.current.threadId !== targetThread) return;
        if (response.outcome !== "accepted") { setNotice(errorText(response)); return; }
        const turns = response.data?.turns as Turn[] | undefined;
        const nextCursor = response.data?.nextCursor;
        if (!Array.isArray(turns) || typeof nextCursor !== "string") { setNotice("历史页格式无法识别，已保留当前会话。"); return; }
        const next = appendOlderPage(current, selection.current.view?.thread.turns || view.thread.turns, { turns, nextCursor });
        if (next.cursor === current.cursor && !next.exhausted && next.older.length === current.older.length) { setNotice("历史页没有继续前进，请稍后重试。"); return; }
        pendingReadingAnchor.current = captureReadingAnchor(transcriptRef.current, readingKey(deviceId, targetThread));
        historyRef.current = next;
        setHistory(next);
        if (next.older.length >= targetCount || next.exhausted) return;
      }
      setNotice("历史变化过快，请稍后重新加载更早消息。");
    } finally {
      historyLoadingRef.current = false;
      setHistoryLoading(false);
    }
  }

  useEffect(() => {
    if (!view || view.thread.recentComplete !== false || history.exhausted || historyLoadingRef.current) return;
    const missing = recentTurnLimit - view.thread.turns.length - history.older.length;
    if (missing > 0) void loadOlder(missing);
  }, [view?.seq, history.older.length, history.exhausted]);

  async function loadOlderItems(turn: Turn) {
    if (!view || !deviceId || itemLoadingRef.current || turn.itemsComplete !== false || !turn.nextItemCursor) return;
    const epoch = pendingSelect.current;
    const targetThread = view.threadId;
    itemLoadingRef.current = turn.turnId;
    setItemLoading(turn.turnId);
    try {
      const response = await client.request("thread.history.items", deviceId, { threadId: targetThread, turnId: turn.turnId, cursor: turn.nextItemCursor, limit: 100 });
      if (epoch !== pendingSelect.current || selection.current.threadId !== targetThread) return;
      if (response.outcome !== "accepted") { setNotice(errorText(response)); return; }
      const items = response.data?.items as Item[] | undefined;
      const nextItemCursor = response.data?.nextItemCursor;
      const itemsComplete = response.data?.itemsComplete;
      if (response.data?.turnId !== turn.turnId || !Array.isArray(items) || typeof nextItemCursor !== "string" || typeof itemsComplete !== "boolean") { setNotice("消息页格式无法识别，已保留当前内容。"); return; }
      pendingReadingAnchor.current = captureReadingAnchor(transcriptRef.current, readingKey(deviceId, targetThread));
      setItemOverrides(current => ({ ...current, [turn.turnId]: prependOlderItems(current[turn.turnId] || turn, items, nextItemCursor, itemsComplete) }));
    } finally {
      itemLoadingRef.current = "";
      setItemLoading("");
    }
  }

  async function send() {
    if (!view || !deviceId || readOnlyHistory || !canSend(view.thread, status === "ready", working, draft)) return;
    const text = draft;
    setWorking(true); setNotice("");
    const response = await client.request("turn.start", deviceId, { threadId: view.threadId, clientMessageId: newRequestID(), text });
    setWorking(false);
    setDraft(current => current === text ? preserveDraftAfterSend(current, response.outcome) : current);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  async function stop() {
    if (!view || !activeTurn || stopping || readOnlyHistory) return;
    setStopping(true);
    const response = await client.request("turn.interrupt", deviceId, { threadId: view.threadId, expectedTurnId: activeTurn.turnId });
    setStopping(false);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  async function respond(interaction: Interaction, decision: string) {
    if (!view || working || readOnlyHistory) return;
    const raw = answers[interaction.interactionId] || {};
    const mapped = decision === "answer" ? answersForSubmission(interaction, raw) : null;
    if (decision === "answer" && !mapped) return;
    setWorking(true);
    const response = await client.request("interaction.respond", deviceId, { threadId: view.threadId, interactionId: interaction.interactionId, decision, ...(decision === "answer" ? { answers: mapped } : {}) });
    setWorking(false);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  return <div className={`app-shell ${status === "ready" ? "connected" : ""}`} style={visualViewportHeight === null ? undefined : { height: visualViewportHeight }}>
    <header className="masthead">
      <div className="brand"><BrandMark /><span>Ariel</span><small>Codex 随身工作台</small></div>
      <div className="mast-actions"><span className={`connection ${status}`}><span className="status-dot" />{connectionLabel}</span><button className="text-button" onClick={disconnect}>断开</button></div>
    </header>
    {status !== "ready" && <section className="connect-panel" aria-label="连接 Relay"><div><span className="eyebrow">PRIVATE ACCESS</span><h1>继续你的工作，<br />不必守在电脑前。</h1><p>输入 6 位连接码。连接后，同一标签页刷新会自动恢复；连接码不会存入浏览器。</p></div><form onSubmit={e => { e.preventDefault(); if (isWebPIN(token)) { savedSessionAttempt.current = false; setSessionExpired(false); client.connect(token); } }}><label htmlFor="token">6 位连接码</label><div className="connect-row"><input id="token" type="password" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={token} onChange={e => setToken(e.target.value)} autoComplete="off" placeholder="输入 6 位数字" required /><button className="primary" type="submit" disabled={!isWebPIN(token)}>连接 <span aria-hidden="true">↗</span></button></div>{status === "invalid" && <p role="alert" className="connect-error">{sessionExpired ? "保存的会话已失效，请重新输入连接码。" : "连接码错误或 Relay 已锁定；累计 10 次错误后需重启 Relay。"}</p>}<small>仅建议在可信局域网使用。HTTP/WS 连接未加密。</small></form></section>}
    <div className="workspace">
      {showList && <button className="sidebar-backdrop" type="button" aria-label="关闭会话列表遮罩" onClick={() => setShowList(false)} />}
      <aside id="session-sidebar" className={`sidebar ${showList ? "open" : ""}`} aria-label="会话列表">
        <div className="sidebar-identity"><div className="brand"><BrandMark /><span>Ariel</span></div><span className={`connection ${status}`}><span className="status-dot" />{connectionLabel}</span><button className="sidebar-disconnect text-button" aria-label="断开" onClick={disconnect}>断开</button></div>
        <div className="sidebar-head"><span className="eyebrow">WORKSPACE</span><h2>会话</h2><button className="icon-button mobile-close" aria-label="关闭会话列表" onClick={() => setShowList(false)}>×</button><button className="icon-button" aria-label="刷新会话" onClick={() => void loadThreads(deviceId)} disabled={!deviceId}>↻</button></div>
        <label className="device-label" htmlFor="device">设备</label><select id="device" value={deviceId} onChange={e => setDeviceId(e.target.value)} disabled={status !== "ready"}><option value="">{devices.length ? "选择设备" : "暂无在线设备"}</option>{devices.map(d => <option key={d.deviceId} value={d.deviceId}>{d.deviceName}</option>)}</select>
        {device && <div className="device-meta"><span className={`status-dot ${device.agentOnline ? "online" : ""}`} />{device.agentOnline ? "Agent 在线" : "Agent 离线"}<span>·</span>{device.codexReady ? "Codex 就绪" : mock ? "Mock 演示" : "Codex 未就绪"}</div>}
        <div className="list-caption"><span>最近会话</span><span>{threads.length}</span></div>
        <div className="thread-list">{threads.map(t => <button key={t.threadId} className={`thread-row ${threadId === t.threadId ? "selected" : ""}`} onClick={() => void selectThread(t.threadId)}><span className="thread-title">{t.title || "未命名会话"}</span><span className="thread-path">{t.cwd}</span><span className="thread-date">{new Date(t.updatedAt).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })}</span></button>)}</div>
        {cursor && <button className="load-more" onClick={() => void loadThreads(deviceId, cursor)}>加载更多 →</button>}
        <div className="sidebar-foot">{mock ? "MOCK SESSION · 非真实 Codex 历史" : "原始会话 · 不创建远程副本"}</div>
      </aside>
      <main className="conversation">
        <div className="conversation-head">
          <button className="mobile-list text-button" aria-label={`打开会话列表，${connectionLabel}`} aria-expanded={showList} aria-controls="session-sidebar" onClick={() => setShowList(true)}><span className="menu-glyph" aria-hidden="true">☰</span><span className={`status-dot ${status === "ready" ? "online" : ""}`} aria-hidden="true" /></button>
          <div className="conversation-title"><span className="eyebrow">{mock ? "MOCK DEMO" : "CODEX SESSION"}</span><h2>{view?.thread.title || selectedThread?.title || "选择一个会话"}</h2><span className="head-path">{view?.thread.cwd || selectedThread?.cwd || "从左侧选择历史会话，接着工作。"}</span></div>
          <div className="head-right">{mock && <span className="mock-badge">模拟环境</span>}{view && <span className="runtime">{view.thread.runtime === "inProgress" ? "运行中" : view.thread.runtime === "idle" ? "待命" : view.thread.runtime === "notLoaded" ? "加载中" : "状态未知"}</span>}</div>
          {currentPermissions && <div className="permission-info" ref={permissionInfoRef}><button className={`permission-info-button ${permissionWarning ? "danger" : ""}`} type="button" aria-label={currentPermissions.label} aria-expanded={permissionInfoOpen} aria-controls="desktop-permission-details" onClick={() => setPermissionInfoOpen(open => !open)}><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 10.5v5"/><path d="M12 7.5h.01"/></svg></button>{permissionInfoOpen && <div id="desktop-permission-details" className="permission-popover" role="dialog" aria-label="当前 Desktop 权限详情"><div className="permission-popover-title">当前 Desktop 权限</div><div>{currentPermissions.label}</div>{currentPermissions.warning && <p>{currentPermissions.warning}</p>}</div>}</div>}
        </div>
        {currentPermissions && <section className={`permission-strip ${currentPermissions.warning ? "danger" : ""}`} aria-label="当前 Desktop 权限" role={currentPermissions.warning ? "alert" : "status"}><span>{currentPermissions.label}</span>{currentPermissions.warning && <span className="permission-note">{currentPermissions.warning}</span>}</section>}
        {readOnlyHistory && view && <div className="readonly-banner" role="status">历史只读 · 当前 Desktop 状态未确认，发送、停止和审批已禁用。</div>}
        <div className="transcript" ref={transcriptRef} onScroll={onTranscriptScroll} aria-live="polite">
          {!view && <div className="empty"><div className="empty-symbol">✳</div><h3>{blockedSelection.current === `${deviceId}\u0000${threadId}` ? "会话状态无法确认" : threadId ? "正在同步会话…" : "从这里接续"}</h3><p>{blockedSelection.current === `${deviceId}\u0000${threadId}` ? "远程操作已暂停。请稍后手动重新选择会话。" : threadId ? "等待电脑端加载原始历史。" : "选一个会话，历史、运行状态与需要你决定的问题会出现在这里。"}</p></div>}
          {view?.thread.historyComplete === false && !history.exhausted && <div className="history-control"><button className="history-action" type="button" onClick={() => void loadOlder()} disabled={historyLoading}>{historyLoading ? "正在加载更早消息…" : "加载更早消息"}</button></div>}
          {displayedTurns.slice(0, history.older.length).map(turn => <ConversationTurn key={`${view?.threadId}:${turn.turnId}`} turn={turn} loading={itemLoading === turn.turnId} onLoadOlderItems={turn => void loadOlderItems(turn)} active={status === "ready" && view?.thread.runtime === "inProgress" && view.thread.pendingInteractions.length === 0 && activeTurn?.turnId === turn.turnId} />)}
          {history.gap && <div className="history-gap">中间消息已从当前浏览窗口释放 <button type="button" onClick={resetHistory}>回到最新</button></div>}
          {displayedTurns.slice(history.older.length).map(turn => <ConversationTurn key={`${view?.threadId}:${turn.turnId}`} turn={turn} loading={itemLoading === turn.turnId} onLoadOlderItems={turn => void loadOlderItems(turn)} active={status === "ready" && view?.thread.runtime === "inProgress" && view.thread.pendingInteractions.length === 0 && activeTurn?.turnId === turn.turnId} />)}
          {view?.thread.pendingInteractions.map(card => <InteractionCard key={card.interactionId} card={card} values={answers[card.interactionId] || {}} onChange={(id, value) => setAnswers(all => ({ ...all, [card.interactionId]: { ...all[card.interactionId], [id]: value } }))} onRespond={decision => void respond(card, decision)} disabled={working} />)}
        </div>
        {showReturnToLatest && view && <div className="return-latest-bar"><button type="button" onClick={returnToLatest}>回到最新</button></div>}
        <div className="composer-wrap">{notice && <div className="notice" role="alert"><span>!</span>{notice}<button aria-label="关闭提示" onClick={() => setNotice("")}>×</button></div>}<div className="composer"><textarea ref={composerInputRef} aria-label="发送消息" enterKeyHint="enter" placeholder={view ? view.thread.runtime === "inProgress" ? "Codex 正在运行；你可以先写草稿…" : "给 Codex 发消息…" : "选择会话后开始输入…"} value={draft} onChange={e => setDraft(e.target.value)} onKeyDown={e => { if (window.innerWidth > mobileViewportMaxWidth && e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(); } }} disabled={!view || status !== "ready" || readOnlyHistory} rows={1} /><div className="composer-actions"><span>Enter 发送 · Shift+Enter 换行</span><div>{activeTurn && <button className="stop-button" onClick={() => void stop()} disabled={stopping || status !== "ready" || readOnlyHistory}>■ 停止</button>}<button className="primary send-button" aria-label="发送" onClick={() => void send()} disabled={readOnlyHistory || !canSend(view?.thread || null, status === "ready", working, draft)}><span className="send-label">发送</span><span className="send-glyph" aria-hidden="true">↑</span></button></div></div></div></div>
      </main>
    </div>
  </div>;
}

function ConversationTurn({ turn, loading, onLoadOlderItems, active }: { turn: Turn; loading: boolean; onLoadOlderItems: (turn: Turn) => void; active: boolean }) {
  const [expandedIDs, setExpandedIDs] = useState<Set<string>>(() => new Set());
  const parts = groupTurnItems(turn);
  const lastPart = parts.at(-1);
  const processing = active && lastPart?.kind === "activity";
  return <section className="turn">
    {turn.status !== "inProgress" && <div className="turn-status">{turn.status === "completed" ? "已完成" : turn.status === "interrupted" ? "已停止" : "失败"}</div>}
    {turn.itemsComplete === false && <button className="history-action item-history-action" type="button" onClick={() => onLoadOlderItems(turn)} disabled={loading}>{loading ? "正在加载…" : "加载此回合更早内容"}</button>}
    {parts.map(part => {
      if (part.kind === "message") {
        const item = part.item;
        return <article className={`message ${item.role}`} aria-label={item.role === "user" ? "你" : item.role === "assistant" ? "Codex" : "系统"} key={item.itemId} data-item-id={item.itemId}><div className="message-body"><div className="message-text">{item.role === "system" ? item.text : <ConversationMarkdown text={item.text} />}</div></div></article>;
      }
      const ids = part.items.map(item => item.itemId);
      const expanded = ids.some(id => expandedIDs.has(id));
      const running = processing && part === lastPart;
      const controlID = `activity-${turn.turnId}-${ids[0]}`;
      return <section className="activity-group" key={ids[0]} data-item-id={ids.at(-1)}>
        <button className="activity-summary" type="button" aria-expanded={expanded} aria-controls={controlID} onClick={() => setExpandedIDs(current => { const next = new Set(current); for (const id of ids) { if (expanded) next.delete(id); else next.add(id); } return next; })}><span className="activity-chevron" aria-hidden="true">{expanded ? "⌄" : "›"}</span>{running ? `处理中… · ${ids.length} 项` : `已处理 ${ids.length} 项`}</button>
        {expanded && <div className="activity-list" id={controlID}>{part.firstLoaded && <p className="activity-note">此回合还有更早内容，可在上方按需加载。</p>}{part.items.map(item => <div className="activity-item" key={item.itemId} data-item-id={item.itemId}><div className="activity-item-head"><span>{item.activity!.label}</span><span>{activityStatusText(item.activity!.status)}</span></div><div className="activity-kind">{item.activity!.kind}</div>{item.activity!.details ? <pre className="activity-details">{item.activity!.details}</pre> : <p className="activity-note">当前历史没有更多详情。</p>}{item.activity!.truncated && <p className="activity-note">详情已截断；完整内容请在原 Codex Desktop 查看。</p>}</div>)}</div>}
      </section>;
    })}
    {active && !processing && <div className="agent-progress" role="status">思考中…</div>}
  </section>;
}

export function InteractionCard({ card, values, onChange, onRespond, disabled }: { card: Interaction; values: Record<string, string>; onChange: (id: string, value: string) => void; onRespond: (decision: string) => void; disabled: boolean }) {
  return <section className="interaction-card"><span className="eyebrow">NEEDS YOUR INPUT</span><h3>{card.kind === "command_approval" ? "等待命令审批" : card.kind === "file_approval" ? "等待文件变更审批" : card.kind === "permission_request" ? "等待权限请求" : card.kind === "user_input" ? "Codex 有一个问题" : "暂不支持的交互"}</h3><p>{card.prompt}</p>{card.questions?.map(q => <label key={q.id} className="question">{q.question}<input list={`options-${card.interactionId}-${q.id}`} value={values[q.id] || ""} onChange={e => onChange(q.id, e.target.value)} placeholder={q.options?.length ? "选择建议或自行输入" : "输入回答"} /><datalist id={`options-${card.interactionId}-${q.id}`}>{q.options?.map(o => <option key={o} value={o} />)}</datalist></label>)}<div className="interaction-actions">{card.availableDecisions.map(decision => <button key={decision} className={decision === "accept_once" || decision === "answer" ? "primary" : "secondary"} disabled={disabled || (decision === "answer" && !answersForSubmission(card, values))} onClick={() => onRespond(decision)}>{decision === "accept_once" && card.kind === "permission_request" ? "仅本轮按原请求授权" : ({ accept_once: "仅本次允许", deny: "拒绝", deny_and_stop: "拒绝并停止", answer: "提交回答" } as Record<string, string>)[decision]}</button>)}</div></section>;
}
