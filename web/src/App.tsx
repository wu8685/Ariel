import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { ArielSocket, isWebPIN, type ConnectionStatus } from "./client";
import { applyThreadEvent, belongsToSubscription, keepOfflineDevice, preserveDraftAfterSend, recoveryTarget, permissionSummary, canSend, type ThreadView } from "./state";
import { answersForSubmission } from "./interaction";
import { newRequestID } from "./ids";
import { WebSession } from "./session";
import { appendOlderPage, emptyHistoryState, prependOlderItems, type HistoryState } from "./history";
import { activityStatusText, groupTurnItems } from "./activity";
import { ConversationMarkdown } from "./markdown";
import { ConversationImage, readScreenshotFiles, type ScreenshotDraft } from "./screenshots";
import { isPairingCredential, pairingQRCode, pairingURL, takePairingCredential } from "./pairing";
import { groupThreadsByProject, mergeThreadPages, partitionThreadsByPin } from "./projects";
import { reorderQueueAtPointer } from "./queue-order";
import { ArielLogo } from "./ArielLogo";
import type { ArielProtocolV1Envelope, Thread, Turn, Item, Response, Interaction, QueuedMessage } from "./generated/protocol";
import "./interaction.css";

type Device = { deviceId: string; deviceName: string; agentOnline: boolean; codexReady: boolean; agentEpoch?: string; adapterVersion?: string; capabilities: { autoLoad: boolean; history?: boolean; send?: boolean; interrupt?: boolean; interaction?: boolean; queue?: boolean; threadCreate?: boolean } };
const wsURL = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws`;
const recentTurnLimit = 10;
const mobileViewportMaxWidth = 800;
const mobileComposerMinHeight = 44;
const mobileComposerMaxHeight = 24 * 8 + 20; // Eight 24px lines plus vertical padding.
const latestFollowDistance = 80;
const historyHeaderExpandDistance = 32;
const queueMenuWidth = 108;
const queueMenuRowHeight = 34;
type ReadingAnchor = { key: string; itemId: string; top: number; scrollTop: number; scrollHeight: number };
type PhonePairing = { credential: string; phase: "confirm" | "connecting" | "error" };
type PairingInvite = { status: "loading" | "waiting" | "consumed" | "expired" | "error"; credential: string; expiresAt: number; qr: string; message: string };
type QueueDragState = { pointerId: number; queueId: string; startY: number; order: string[]; cleanup: () => void };

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
const resultText: Record<string, string> = { DEVICE_OFFLINE: "设备离线，请确认电脑上的 Agent 已连接。", TURN_BUSY: "这个会话正在运行；草稿已保留，不会自动重发。", STALE_TURN: "运行中的 turn 已变化，请刷新状态后再停止。", STALE_INTERACTION: "这项交互已经变化或过期，请查看最新会话状态。", OUTCOME_UNKNOWN: "执行结果不确定。请先查看会话状态，不要直接重发。", RESYNC_REQUIRED: "事件顺序发生变化，正在重新同步。", NATIVE_STATE_UNCERTAIN: "Codex 原生会话状态暂时无法确认，已停止此会话的远程操作。请稍后手动重新选择；若持续出现，请在电脑端查看。", HISTORY_TOO_LARGE: "此页内容超过安全传输上限；已保留当前可见内容。", INTERACTION_UNSUPPORTED: "这张卡片已失效或当前决定不可用。", INVALID_ARGUMENT: "请求内容无效。", OVERLOADED: "请求过多，请稍后再试。", PROTOCOL_UNSUPPORTED: "当前 Codex 版本不支持这项操作。" };

function errorText(response: Response): string {
  return response.error ? resultText[response.error.code] || response.error.message : "操作未完成。";
}

function responseQueue(response: Response): QueuedMessage[] | null {
  const items = response.data?.queuedMessages;
  if (!Array.isArray(items)) return null;
  for (const item of items) {
    if (!item || typeof item !== "object") return null;
    const value = item as Record<string, unknown>;
    if (typeof value.queueId !== "string" || !value.queueId || typeof value.clientMessageId !== "string" || !value.clientMessageId || typeof value.text !== "string" || typeof value.editable !== "boolean" || !Array.isArray(value.images) || value.images.length > 3 || value.images.some(image => typeof image !== "string")) return null;
  }
  return items as QueuedMessage[];
}

function QueueHandleIcon() {
  return <svg aria-hidden="true" viewBox="0 0 16 18" fill="currentColor"><circle cx="5" cy="4" r="1.2"/><circle cx="11" cy="4" r="1.2"/><circle cx="5" cy="9" r="1.2"/><circle cx="11" cy="9" r="1.2"/><circle cx="5" cy="14" r="1.2"/><circle cx="11" cy="14" r="1.2"/></svg>;
}

function QueueGuideIcon() {
  return <svg aria-hidden="true" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M3 5v3.5A3.5 3.5 0 0 0 6.5 12H15"/><path d="m12 9 3 3-3 3"/></svg>;
}

function QueueTrashIcon() {
  return <svg aria-hidden="true" viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M3.5 5h11M7 2.75h4M5 5l.7 10h6.6L13 5M7.5 7.5v5M10.5 7.5v5"/></svg>;
}

function PhonePairingPanel({ pairing, origin, onConfirm, onCancel }: { pairing: PhonePairing; origin: string; onConfirm: () => void; onCancel: () => void }) {
  const error = pairing.phase === "error";
  return <section className="connect-panel pairing-confirm" aria-label="确认扫码登录">
    <div><span className="eyebrow">ONE-TIME PAIRING</span><h1>确认登录 Ariel</h1><p>你正在登录 <strong>{origin}</strong>。只有你亲自扫描并确认时才继续。</p></div>
    <div className="pairing-confirm-card">
      <div className="pairing-phone-mark" aria-hidden="true">⌁</div>
      <h2>{error ? "二维码已失效" : pairing.phase === "connecting" ? "正在建立安全会话…" : "在这台手机上继续？"}</h2>
      <p>{error ? "配对可能已过期、取消、使用过，或连接结果未知。请重新扫码。" : "确认后会直接进入 Ariel；6 位连接码不会传到这台手机。"}</p>
      <div className="pairing-actions">
        {!error && <button className="primary" type="button" onClick={onConfirm} disabled={pairing.phase === "connecting"}>确认在此手机登录</button>}
        <button className="secondary" type="button" onClick={onCancel}>{error ? "使用连接码登录" : "取消"}</button>
      </div>
      <small>一次性凭据不可重放；仅在可信局域网使用。</small>
    </div>
  </section>;
}

function PairingDialog({ invite, seconds, onClose, onRegenerate }: { invite: PairingInvite; seconds: number; onClose: () => void; onRegenerate: () => void }) {
  return <div className="pairing-backdrop" role="presentation">
    <section className="pairing-dialog" role="dialog" aria-modal="true" aria-labelledby="pairing-title">
      <button className="pairing-close icon-button" type="button" aria-label="关闭扫码登录" onClick={onClose}>×</button>
      <span className="eyebrow">ONE-TIME PAIRING</span>
      <h2 id="pairing-title">手机扫码登录</h2>
      {invite.status === "loading" && <div className="pairing-state" role="status">正在生成一次性二维码…</div>}
      {invite.status === "waiting" && <>
        <div className="pairing-qr"><img src={invite.qr} alt="手机扫码登录二维码" /></div>
        <p>用手机系统相机扫描，然后在手机上点击确认。</p>
        <strong className="pairing-countdown">{seconds} 秒后失效</strong>
        <small>二维码不包含 6 位连接码；旁观者抢先扫描仍可能占用本次邀请。</small>
      </>}
      {invite.status === "consumed" && <div className="pairing-state success"><strong>手机已登录</strong><span>这张二维码已经失效，不能再次使用。</span></div>}
      {invite.status === "expired" && <div className="pairing-state"><strong>二维码已过期</strong><span>请重新生成后再扫描。</span></div>}
      {invite.status === "error" && <div className="pairing-state" role="alert"><strong>{invite.message}</strong></div>}
      <div className="pairing-actions">
        {(invite.status === "expired" || invite.status === "error") && <button className="primary" type="button" onClick={onRegenerate}>重新生成</button>}
        <button className="secondary" type="button" onClick={onClose}>关闭</button>
      </div>
    </section>
  </div>;
}

export function App({ initialPairingCredential }: { initialPairingCredential?: string } = {}) {
  const client = useMemo(() => new ArielSocket(wsURL), []);
  const webSession = useMemo(() => new WebSession(() => window.sessionStorage), []);
  const [phonePairing, setPhonePairing] = useState<PhonePairing | null>(() => {
    const credential = initialPairingCredential ?? takePairingCredential(window.location, window.history);
    return credential ? { credential, phase: "confirm" } : null;
  });
  const pairingAttempt = useRef(false);
  const [pairingInvite, setPairingInvite] = useState<PairingInvite | null>(null);
  const [pairingSeconds, setPairingSeconds] = useState(0);
  const [token, setToken] = useState("");
  const [sessionExpired, setSessionExpired] = useState(false);
  const savedSessionAttempt = useRef(false);
  const [status, setStatus] = useState<ConnectionStatus>("disconnected");
  const [devices, setDevices] = useState<Device[]>([]);
  const [deviceId, setDeviceId] = useState("");
  const [threads, setThreads] = useState<Thread[]>([]);
  const [cursor, setCursor] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [searchTerm, setSearchTerm] = useState("");
  const [searchRevision, setSearchRevision] = useState(0);
  const [listLoading, setListLoading] = useState(false);
  const [listError, setListError] = useState("");
  const [expandedProjectKeys, setExpandedProjectKeys] = useState<Set<string>>(() => new Set());
  const [newThreadOpen, setNewThreadOpen] = useState(false);
  const [newThreadCwd, setNewThreadCwd] = useState("");
  const [newThreadError, setNewThreadError] = useState("");
  const [creatingThread, setCreatingThread] = useState(false);
  const searchInputRef = useRef("");
  const searchInputChanged = useRef(false);
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
  const [screenshots, setScreenshots] = useState<ScreenshotDraft[]>([]);
  const screenshotInputRef = useRef<HTMLInputElement>(null);
  const [notice, setNotice] = useState("");
  const [working, setWorking] = useState(false);
  const [queueWorking, setQueueWorking] = useState("");
  const [editingQueueId, setEditingQueueId] = useState("");
  const [queueMenuId, setQueueMenuId] = useState("");
  const [queueMenuPosition, setQueueMenuPosition] = useState({ left: 0, top: 0, above: false });
  const [draggingQueueId, setDraggingQueueId] = useState("");
  const [dragQueueOrder, setDragQueueOrder] = useState<string[]>([]);
  const [stopping, setStopping] = useState(false);
  const [showList, setShowList] = useState(true);
  const [showReturnToLatest, setShowReturnToLatest] = useState(false);
  const [historyChromeCollapsed, setHistoryChromeCollapsed] = useState(false);
  const [permissionInfoOpen, setPermissionInfoOpen] = useState(false);
  const [visualViewportHeight, setVisualViewportHeight] = useState<number | null>(null);
  const [answers, setAnswers] = useState<Record<string, Record<string, string>>>({});
  const selection = useRef({ deviceId: "", threadId: "", view: null as ThreadView | null });
  const expectedSubscription = useRef("");
  const blockedSelection = useRef("");
  const pendingSelect = useRef(0);
  const deviceListGeneration = useRef(0);
  const threadListGeneration = useRef(0);
  const threadCreateGeneration = useRef(0);
  const resuming = useRef(false);
  const transcriptRef = useRef<HTMLDivElement>(null);
  const followLatestRef = useRef(true);
  const lastTranscriptScrollTop = useRef(0);
  const pendingReadingAnchor = useRef<ReadingAnchor | null>(null);
  const resumeReadingAnchor = useRef<ReadingAnchor | null>(null);
  const lastPaint = useRef<{ key: string; seq: number; marker: ContentMarker | null }>({ key: "", seq: -1, marker: null });
  const permissionInfoRef = useRef<HTMLDivElement>(null);
  const composerInputRef = useRef<HTMLTextAreaElement>(null);
  const pairingGeneration = useRef(0);
  const queueDrag = useRef<QueueDragState | null>(null);
  const device = devices.find(d => d.deviceId === deviceId);
  const selectedThread = threads.find(t => t.threadId === threadId);
  const allThreadGroups = useMemo(() => groupThreadsByProject(threads), [threads]);
  const { pinned: pinnedThreads, regular: regularThreads } = useMemo(() => partitionThreadsByPin(threads), [threads]);
  const threadGroups = useMemo(() => groupThreadsByProject(regularThreads), [regularThreads]);
  const mock = device?.adapterVersion?.startsWith("mock-") ?? false;
  const activeTurn = view?.thread.turns.findLast(t => t.status === "inProgress");
  const queuedMessages = view?.thread.queuedMessages || [];
  const queueEnabled = device?.capabilities.queue === true;
  const threadCreateEnabled = device?.capabilities.threadCreate === true;
  const queuePaused = queuedMessages.length > 0 && view?.thread.runtime === "idle" && view.thread.turns.at(-1)?.status === "interrupted";
  const sendAction = editingQueueId ? "保存排队消息" : view?.thread.runtime === "inProgress" && queueEnabled ? "加入队列" : "发送";
  const renderedQueuedMessages = dragQueueOrder.length === queuedMessages.length
    ? dragQueueOrder.map(id => queuedMessages.find(item => item.queueId === id)).filter((item): item is QueuedMessage => !!item)
    : queuedMessages;
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
    const scrollingTowardLatest = container.scrollTop > lastTranscriptScrollTop.current + 1;
    lastTranscriptScrollTop.current = container.scrollTop;
    const distanceFromLatest = Math.max(0, container.scrollHeight - container.clientHeight - container.scrollTop);
    if (historyChromeCollapsed) {
      const returnedToLatest = scrollingTowardLatest && distanceFromLatest <= historyHeaderExpandDistance;
      followLatestRef.current = returnedToLatest;
      if (returnedToLatest) {
        setHistoryChromeCollapsed(false);
        setShowReturnToLatest(false);
      }
      return;
    }
    followLatestRef.current = followLatestRef.current
      ? distanceFromLatest <= latestFollowDistance
      : distanceFromLatest <= historyHeaderExpandDistance;
    setHistoryChromeCollapsed(collapsed => collapsed
      ? distanceFromLatest > historyHeaderExpandDistance
      : distanceFromLatest > latestFollowDistance);
    if (followLatestRef.current) setShowReturnToLatest(false);
  }

  function returnToLatest() {
    followLatestRef.current = true;
    setShowReturnToLatest(false);
    setHistoryChromeCollapsed(false);
    const container = transcriptRef.current;
    if (container) { scrollToLatest(container); lastTranscriptScrollTop.current = container.scrollTop; }
  }

  function disconnect() {
    closePairingInvite();
    webSession.disconnect(); savedSessionAttempt.current = false; pairingAttempt.current = false; setSessionExpired(false); setToken(""); client.disconnect();
  }

  function cancelPairingCredential(credential: string) {
    if (isPairingCredential(credential) && status === "ready") void client.request("auth.pair.cancel", "relay", { credential });
  }

  function closePairingInvite() {
    pairingGeneration.current++;
    if (pairingInvite?.status === "waiting") cancelPairingCredential(pairingInvite.credential);
    setPairingInvite(null);
  }

  async function openPairingInvite() {
    const generation = ++pairingGeneration.current;
    if (pairingInvite?.status === "waiting") cancelPairingCredential(pairingInvite.credential);
    setPairingInvite({ status: "loading", credential: "", expiresAt: 0, qr: "", message: "" });
    const response = await client.request("auth.pair.create", "relay", {});
    if (generation !== pairingGeneration.current) {
      const staleCredential = response.data?.credential;
      if (typeof staleCredential === "string") cancelPairingCredential(staleCredential);
      return;
    }
    const credential = response.data?.credential;
    const expiresAtText = response.data?.expiresAt;
    const expiresAt = typeof expiresAtText === "string" ? Date.parse(expiresAtText) : Number.NaN;
    if (response.outcome !== "accepted" || typeof credential !== "string" || !isPairingCredential(credential) || !Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
      setPairingInvite({ status: "error", credential: "", expiresAt: 0, qr: "", message: "无法创建配对，请重试。" });
      return;
    }
    try {
      const qr = await pairingQRCode(pairingURL(window.location.origin, credential));
      if (generation !== pairingGeneration.current) { cancelPairingCredential(credential); return; }
      setPairingInvite({ status: "waiting", credential, expiresAt, qr, message: "" });
    } catch {
      cancelPairingCredential(credential);
      setPairingInvite({ status: "error", credential: "", expiresAt: 0, qr: "", message: "二维码生成失败，请重试。" });
    }
  }

  function confirmPhonePairing() {
    if (!phonePairing || phonePairing.phase !== "confirm") return;
    pairingAttempt.current = true;
    savedSessionAttempt.current = false;
    setPhonePairing({ ...phonePairing, phase: "connecting" });
    client.connect(phonePairing.credential);
  }

  function cancelPhonePairing() {
    pairingAttempt.current = false;
    client.disconnect();
    setPhonePairing(null);
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

  async function loadThreads(id: string, next = "", query = searchTerm) {
    const generation = ++threadListGeneration.current;
    if (!id) { setThreads([]); setListLoading(false); return; }
    setListLoading(true);
    setListError("");
    const response = await client.request("thread.list", id, { limit: 50, ...(next ? { cursor: next } : {}), ...(query ? { searchTerm: query } : {}) });
    if (generation !== threadListGeneration.current || selection.current.deviceId !== id || searchInputRef.current.trim() !== query) return;
    setListLoading(false);
    if (response.outcome !== "accepted") {
      setListError(query && response.error?.code === "PROTOCOL_UNSUPPORTED" ? "当前 Codex 版本不支持会话内容搜索" : errorText(response));
      return;
    }
    const page = (response.data?.threads as Thread[] | undefined) || [];
    setThreads(current => next ? mergeThreadPages(current, page) : page);
    setCursor(String(response.data?.nextCursor || ""));
  }

  function openNewThreadDialog() {
    if (!threadCreateEnabled || !deviceId) return;
    setNewThreadCwd(view?.thread.cwd || selectedThread?.cwd || allThreadGroups[0]?.path || "");
    setNewThreadError("");
    setNewThreadOpen(true);
  }

  function toggleProjectGroup(key: string) {
    setExpandedProjectKeys(current => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function closeNewThreadDialog() {
    if (creatingThread) return;
    threadCreateGeneration.current++;
    setNewThreadOpen(false);
    setNewThreadError("");
  }

  async function createThread() {
    const cwd = newThreadCwd.trim();
    if (!threadCreateEnabled || !deviceId || !cwd || creatingThread) return;
    const generation = ++threadCreateGeneration.current;
    const targetDevice = deviceId;
    setCreatingThread(true);
    setNewThreadError("");
    const response = await client.request("thread.create", targetDevice, { cwd });
    if (generation !== threadCreateGeneration.current || targetDevice !== selection.current.deviceId) return;
    setCreatingThread(false);
    if (response.outcome !== "accepted") {
      setNewThreadError(response.error?.code === "INVALID_ARGUMENT" ? "目录必须是这台电脑上已存在的绝对文件夹路径。" : errorText(response));
      return;
    }
    const candidate = response.data?.thread as Partial<Thread> | undefined;
    if (!candidate || typeof candidate.threadId !== "string" || !candidate.threadId || typeof candidate.cwd !== "string" || !candidate.cwd || typeof candidate.title !== "string" || typeof candidate.updatedAt !== "string" || !Array.isArray(candidate.turns) || !Array.isArray(candidate.pendingInteractions)) {
      setNewThreadError("创建结果无法确认。请先在 Codex Desktop 中核对，不要直接重试。");
      return;
    }
    const created = candidate as Thread;
    searchInputRef.current = "";
    searchInputChanged.current = false;
    setSearchInput("");
    setSearchTerm("");
    setCursor("");
    setThreads(current => [created, ...current.filter(thread => thread.threadId !== created.threadId)]);
    setNewThreadOpen(false);
    setShowList(false);
    await selectThread(created.threadId, targetDevice);
    composerInputRef.current?.focus();
  }

  function updateSearch(value: string) {
    searchInputRef.current = value;
    searchInputChanged.current = true;
    threadListGeneration.current++;
    setSearchInput(value);
    setThreads([]);
    setCursor("");
    setListError("");
    setListLoading(status === "ready" && !!deviceId);
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
    else { resumeReadingAnchor.current = null; followLatestRef.current = true; lastTranscriptScrollTop.current = 0; setShowReturnToLatest(false); setHistoryChromeCollapsed(false); }
    pendingReadingAnchor.current = null;
    blockedSelection.current = "";
    const epoch = ++pendingSelect.current;
    const old = selection.current.view;
    const oldDevice = selection.current.deviceId;
    const oldSubscription = expectedSubscription.current;
    expectedSubscription.current = "";
    selection.current = { deviceId: targetDevice, threadId: id, view: null };
    setThreadId(id); setView(null); setReadOnlyHistory(false); setNotice(""); setShowList(false); setScreenshots([]); setEditingQueueId(""); setQueueWorking(""); setQueueMenuId(""); cancelQueueDrag(); resetHistory();
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
        if (pairingAttempt.current) {
          setPhonePairing(current => current ? { credential: "", phase: "error" } : current);
        } else if (savedSessionAttempt.current) setSessionExpired(true);
        pairingAttempt.current = false;
        savedSessionAttempt.current = false;
      }
      if (next !== "ready") { rememberReadingPosition(); pendingSelect.current++; deviceListGeneration.current++; threadListGeneration.current++; threadCreateGeneration.current++; expectedSubscription.current = ""; selection.current.view = null; setView(null); setReadOnlyHistory(false); setNewThreadOpen(false); setCreatingThread(false); resetHistory(); setPairingInvite(current => current?.status === "waiting" ? null : current); }
    };
    client.onReady = (_epoch, sessionToken) => {
      if (sessionToken) webSession.accepted(sessionToken);
      if (pairingAttempt.current) setPhonePairing(null);
      pairingAttempt.current = false;
      savedSessionAttempt.current = Boolean(sessionToken);
      setSessionExpired(false);
      expectedSubscription.current = ""; selection.current.view = null; setView(null); setReadOnlyHistory(false); resetHistory();
      void refreshDevices().then(online => void resumeSelected(online));
    };
    client.onEvent = (event: ArielProtocolV1Envelope) => {
      if (event.type !== "event") return;
      if (event.event === "auth.pair.consumed") {
        setPairingInvite(current => current?.status === "waiting" ? { ...current, status: "consumed", credential: "", qr: "" } : current);
        return;
      }
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
        setThreads(list => list.map(t => t.threadId === next.threadId ? { ...next.thread, searchSnippet: t.searchSnippet } : t));
      }
    };
    if (!phonePairing) {
      const saved = webSession.saved();
      if (saved) { savedSessionAttempt.current = true; client.connect(saved); }
    }
    return () => client.disconnect();
  }, [client]);

  useEffect(() => {
    if (pairingInvite?.status !== "waiting") { setPairingSeconds(0); return; }
    const tick = () => {
      const seconds = Math.max(0, Math.ceil((pairingInvite.expiresAt - Date.now()) / 1000));
      setPairingSeconds(seconds);
      if (seconds === 0) {
        cancelPairingCredential(pairingInvite.credential);
        setPairingInvite(current => current?.credential === pairingInvite.credential ? { ...current, status: "expired", credential: "", qr: "" } : current);
      }
    };
    tick();
    const timer = window.setInterval(tick, 1000);
    return () => window.clearInterval(timer);
  }, [pairingInvite?.status, pairingInvite?.credential, pairingInvite?.expiresAt]);

  useEffect(() => {
    pendingSelect.current++;
    threadListGeneration.current++;
    const oldDevice = selection.current.deviceId;
    if (oldDevice && oldDevice !== deviceId) { resumeReadingAnchor.current = null; followLatestRef.current = true; setShowReturnToLatest(false); }
    const oldSubscription = expectedSubscription.current;
    if (oldDevice && oldSubscription) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    expectedSubscription.current = "";
    selection.current = { deviceId, threadId: "", view: null };
    threadCreateGeneration.current++; setNewThreadOpen(false); setCreatingThread(false); setExpandedProjectKeys(new Set()); setThreadId(""); setView(null); setReadOnlyHistory(false); setThreads([]); setCursor(""); setListError(""); setEditingQueueId(""); setQueueWorking(""); setQueueMenuId(""); cancelQueueDrag(); resetHistory();
  }, [deviceId]);

  useEffect(() => {
    if (!searchInputChanged.current) return;
    const timer = window.setTimeout(() => {
      setSearchTerm(searchInput.trim());
      setSearchRevision(current => current + 1);
    }, 250);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  useEffect(() => {
    if (status === "ready" && deviceId) void loadThreads(deviceId, "", searchTerm);
  }, [deviceId, status, searchRevision]);

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
    if (!newThreadOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !creatingThread) closeNewThreadDialog();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [newThreadOpen, creatingThread]);

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
    if (editingQueueId && !queuedMessages.some(item => item.queueId === editingQueueId)) {
      setEditingQueueId("");
      setDraft("");
      setScreenshots([]);
    }
  }, [editingQueueId, queuedMessages]);

  useEffect(() => {
    if (!queueMenuId) return;
    const close = (event: PointerEvent) => {
      if (!(event.target instanceof Element) || (!event.target.closest(".queue-more-wrap") && !event.target.closest(".queue-menu"))) setQueueMenuId("");
    };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") setQueueMenuId(""); };
    const closeForLayoutChange = () => setQueueMenuId("");
    document.addEventListener("pointerdown", close);
    document.addEventListener("scroll", closeForLayoutChange, true);
    window.addEventListener("keydown", escape);
    window.addEventListener("resize", closeForLayoutChange);
    return () => { document.removeEventListener("pointerdown", close); document.removeEventListener("scroll", closeForLayoutChange, true); window.removeEventListener("keydown", escape); window.removeEventListener("resize", closeForLayoutChange); };
  }, [queueMenuId]);

  useEffect(() => {
    if (!dragQueueOrder.length) return;
    const current = queuedMessages.map(item => item.queueId);
    if (current.length !== dragQueueOrder.length || current.some(id => !dragQueueOrder.includes(id))) {
      cancelQueueDrag();
    }
  }, [queuedMessages, dragQueueOrder]);

  useEffect(() => () => { queueDrag.current?.cleanup(); }, []);

  useLayoutEffect(() => {
    const input = composerInputRef.current;
    if (!input) return;
    const resize = () => {
      const previousScrollTop = input.scrollTop;
      const caretAtEnd = input.selectionEnd === input.value.length;
      input.style.height = "";
      if (window.innerWidth > mobileViewportMaxWidth) return;
      if (!input.value) {
        input.style.height = `${mobileComposerMinHeight}px`;
        input.scrollTop = 0;
        return;
      }
      input.style.height = "auto";
      const contentHeight = input.scrollHeight;
      input.style.height = `${Math.max(mobileComposerMinHeight, Math.min(contentHeight, mobileComposerMaxHeight))}px`;
      input.scrollTop = contentHeight > mobileComposerMaxHeight ? (caretAtEnd ? contentHeight : previousScrollTop) : 0;
    };
    resize();
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, [draft, activeTurn?.turnId]);

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
      if (restoreReadingAnchor(container, resume)) { followLatestRef.current = false; setHistoryChromeCollapsed(true); }
      else { followLatestRef.current = true; setHistoryChromeCollapsed(false); scrollToLatest(container); setNotice("原阅读位置已不在当前历史窗口，已回到最新消息。"); }
      resumeReadingAnchor.current = null;
      lastPaint.current = { key, seq: view.seq, marker };
      return;
    }
    if (lastPaint.current.key !== key) {
      followLatestRef.current = true;
      setShowReturnToLatest(false);
      setHistoryChromeCollapsed(false);
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

  function applyQueueResponse(response: Response, targetDevice: string, targetThread: string, targetEpoch: number): boolean {
    const items = responseQueue(response);
    if (!items || pendingSelect.current !== targetEpoch || selection.current.deviceId !== targetDevice || selection.current.threadId !== targetThread || !selection.current.view) return false;
    const next = { ...selection.current.view, thread: { ...selection.current.view.thread, queuedMessages: items } };
    selection.current.view = next;
    setView(next);
    return true;
  }

  async function send() {
    if (!view || !deviceId || readOnlyHistory || !canSend(view.thread, status === "ready", working || !!queueWorking, draft, screenshots.length, queueEnabled)) return;
    const text = draft;
    const images = screenshots;
    const targetDevice = deviceId;
    const targetThread = view.threadId;
    const targetEpoch = pendingSelect.current;
    const queueID = editingQueueId;
    setWorking(true); setNotice("");
    const content = { text, ...(images.length ? { images: images.map(image => image.dataUri) } : {}) };
    const response = queueID
      ? await client.request("queue.update", targetDevice, { threadId: targetThread, queueId: queueID, ...content })
      : view.thread.runtime === "inProgress" && queueEnabled
        ? await client.request("queue.add", targetDevice, { threadId: targetThread, clientMessageId: newRequestID(), ...content })
        : await client.request("turn.start", targetDevice, { threadId: targetThread, clientMessageId: newRequestID(), ...content });
    setWorking(false);
    const stillSelected = pendingSelect.current === targetEpoch && selection.current.deviceId === targetDevice && selection.current.threadId === targetThread;
    const queueMutation = !!queueID || (view.thread.runtime === "inProgress" && queueEnabled);
    const accepted = response.outcome === "accepted" && (!queueMutation || applyQueueResponse(response, targetDevice, targetThread, targetEpoch));
    if (stillSelected) {
      setDraft(current => current === text ? preserveDraftAfterSend(current, accepted ? "accepted" : response.outcome === "accepted" ? "unknown" : response.outcome) : current);
      if (accepted) {
        setScreenshots(current => current === images ? [] : current);
        if (queueID) setEditingQueueId("");
      }
      if (!accepted) setNotice(response.outcome === "accepted" ? "队列回执格式无法确认，草稿已保留。" : errorText(response));
    }
  }

  function editQueuedMessage(item: QueuedMessage) {
    if (!item.editable || working || queueWorking) return;
    if (!editingQueueId && (draft.trim() || screenshots.length)) {
      setNotice("请先发送或清空当前草稿，再编辑排队消息。");
      return;
    }
    setEditingQueueId(item.queueId);
    setQueueMenuId("");
    setDraft(item.text);
    setScreenshots(item.images.map((dataUri, index) => ({ name: `排队截图 ${index + 1}`, bytes: Math.ceil(dataUri.length * .75), dataUri })));
    setNotice("");
    composerInputRef.current?.focus();
  }

  function cancelQueueEdit() {
    setEditingQueueId("");
    setDraft("");
    setScreenshots([]);
    setNotice("");
  }

  async function mutateQueue(method: "queue.delete" | "queue.reorder" | "queue.steer", params: Record<string, unknown>, marker: string) {
    if (!view || !deviceId || working || queueWorking || readOnlyHistory) return;
    const targetDevice = deviceId;
    const targetThread = view.threadId;
    const targetEpoch = pendingSelect.current;
    setQueueWorking(marker);
    setNotice("");
    const response = await client.request(method, targetDevice, { threadId: targetThread, ...params });
    setQueueWorking("");
    if (pendingSelect.current !== targetEpoch || selection.current.deviceId !== targetDevice || selection.current.threadId !== targetThread) return;
    if (response.outcome !== "accepted") {
      setNotice(method === "queue.steer" && response.error?.code === "PROTOCOL_UNSUPPORTED"
        ? "当前 Codex Desktop 不支持立即引导；排队项仍保留。"
        : errorText(response));
      return;
    }
    if (!applyQueueResponse(response, targetDevice, targetThread, targetEpoch)) { setNotice("队列回执格式无法确认，请等待状态同步后再操作。"); return; }
    if (params.queueId === editingQueueId) cancelQueueEdit();
  }

  function deleteQueuedMessage(item: QueuedMessage) {
    setQueueMenuId("");
    void mutateQueue("queue.delete", { queueId: item.queueId }, `delete:${item.queueId}`);
  }

  function moveQueuedMessage(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= queuedMessages.length) return;
    const queueIds = queuedMessages.map(item => item.queueId);
    [queueIds[index], queueIds[target]] = [queueIds[target], queueIds[index]];
    setQueueMenuId("");
    void mutateQueue("queue.reorder", { queueIds }, `reorder:${queuedMessages[index].queueId}`);
  }

  function toggleQueueMenu(item: QueuedMessage, event: ReactMouseEvent<HTMLButtonElement>) {
    if (queueMenuId === item.queueId) { setQueueMenuId(""); return; }
    const rect = event.currentTarget.getBoundingClientRect();
    const width = queueMenuWidth;
    const estimatedHeight = (item.editable ? 3 : 2) * queueMenuRowHeight + 10;
    const above = rect.top >= estimatedHeight + 8;
    setQueueMenuPosition({
      left: Math.max(8, Math.min(rect.right - width, window.innerWidth - width - 8)),
      top: above ? rect.top - 4 : rect.bottom + 4,
      above,
    });
    setQueueMenuId(item.queueId);
  }

  function beginQueueDrag(item: QueuedMessage, event: ReactPointerEvent<HTMLButtonElement>) {
    if (working || queueWorking) return;
    cancelQueueDrag();
    const order = queuedMessages.map(queued => queued.queueId);
    const move = (pointerEvent: PointerEvent) => updateQueueDrag(pointerEvent);
    const up = (pointerEvent: PointerEvent) => finishQueueDrag(pointerEvent.pointerId, true);
    const cancel = (pointerEvent: PointerEvent) => finishQueueDrag(pointerEvent.pointerId, false);
    const cleanup = () => {
      document.removeEventListener("pointermove", move);
      document.removeEventListener("pointerup", up);
      document.removeEventListener("pointercancel", cancel);
    };
    queueDrag.current = { pointerId: event.pointerId, queueId: item.queueId, startY: event.clientY, order, cleanup };
    document.addEventListener("pointermove", move, { passive: false });
    document.addEventListener("pointerup", up);
    document.addEventListener("pointercancel", cancel);
    setQueueMenuId("");
  }

  function updateQueueDrag(event: PointerEvent) {
    const drag = queueDrag.current;
    if (!drag || drag.pointerId !== event.pointerId || Math.abs(event.clientY - drag.startY) < 4) return;
    event.preventDefault();
    setDraggingQueueId(drag.queueId);
    const rows = Array.from(document.querySelectorAll<HTMLElement>(".queue-list .queue-item[data-queue-id]"), row => {
      const rect = row.getBoundingClientRect();
      return { queueId: row.dataset.queueId || "", top: rect.top, bottom: rect.bottom };
    });
    const next = reorderQueueAtPointer(drag.order, drag.queueId, event.clientY, rows);
    if (next.every((id, index) => id === drag.order[index])) return;
    drag.order = next;
    setDragQueueOrder(next);
  }

  function cancelQueueDrag() {
    const drag = queueDrag.current;
    queueDrag.current = null;
    drag?.cleanup();
    setDraggingQueueId("");
    setDragQueueOrder([]);
  }

  function finishQueueDrag(pointerId: number, submit: boolean) {
    const drag = queueDrag.current;
    if (!drag || drag.pointerId !== pointerId) return;
    cancelQueueDrag();
    const current = queuedMessages.map(item => item.queueId);
    if (submit && (drag.order.length !== current.length || drag.order.some((id, index) => id !== current[index]))) {
      void mutateQueue("queue.reorder", { queueIds: drag.order }, `reorder:${drag.queueId}`);
    }
  }

  function queueHandleKeyDown(index: number, event: ReactKeyboardEvent<HTMLButtonElement>) {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    moveQueuedMessage(index, event.key === "ArrowUp" ? -1 : 1);
  }

  function steerQueuedMessage(item: QueuedMessage) {
    if (!activeTurn || view?.thread.pendingInteractions.length || !item.editable) return;
    setQueueMenuId("");
    void mutateQueue("queue.steer", { queueId: item.queueId, expectedTurnId: activeTurn.turnId }, `steer:${item.queueId}`);
  }

  async function addScreenshots(files: FileList | null) {
    if (!files?.length) return;
    try { setScreenshots(await readScreenshotFiles(screenshots, files)); setNotice(""); }
    catch (error) { setNotice(error instanceof Error ? error.message : "截图无法读取。"); }
    if (screenshotInputRef.current) screenshotInputRef.current.value = "";
  }

  async function loadImage(turnId: string, itemId: string, imageIndex: number): Promise<string> {
    if (!view || !deviceId) throw new Error("会话已切换");
    const response = await client.request("thread.image", deviceId, { threadId: view.threadId, turnId, itemId, imageIndex });
    if (response.outcome !== "accepted" || typeof response.data?.dataUri !== "string") throw new Error(errorText(response));
    return response.data.dataUri;
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
      <div className="brand"><ArielLogo size={29} variant="micro" tone="white" decorative className="brand-logo" /><span>Ariel</span><small>Codex 随身工作台</small></div>
      <div className="mast-actions">{status === "ready" && <button className="text-button pair-entry" type="button" onClick={() => void openPairingInvite()}>手机扫码登录</button>}<span className={`connection ${status}`}><span className="status-dot" />{connectionLabel}</span><button className="text-button" onClick={disconnect}>断开</button></div>
    </header>
    {newThreadOpen && <div className="thread-create-backdrop" role="presentation">
      <section className="thread-create-dialog" role="dialog" aria-modal="true" aria-labelledby="thread-create-title">
        <button className="thread-create-close icon-button" type="button" aria-label="关闭新建会话" onClick={closeNewThreadDialog} disabled={creatingThread}>×</button>
        <span className="eyebrow">NEW CODEX SESSION</span>
        <h2 id="thread-create-title">新建会话</h2>
        <p>选择一个已有项目，或输入这台电脑上的绝对目录。这里只创建空会话，不会自动发送消息。</p>
        <label htmlFor="known-project">已有项目</label>
        <select id="known-project" aria-label="选择已有项目" value={allThreadGroups.some(group => group.path === newThreadCwd) ? newThreadCwd : ""} onChange={event => { if (event.target.value) setNewThreadCwd(event.target.value); }} disabled={creatingThread}>
          <option value="">手动输入其他目录</option>
          {allThreadGroups.filter(group => group.path !== "路径未知").map(group => <option key={group.key} value={group.path}>{group.name} — {group.path}</option>)}
        </select>
        <label htmlFor="thread-create-cwd">项目目录</label>
        <input id="thread-create-cwd" aria-label="项目目录" value={newThreadCwd} maxLength={4096} onChange={event => setNewThreadCwd(event.target.value)} placeholder="/Users/me/workspace/project" disabled={creatingThread} autoCapitalize="none" autoCorrect="off" spellCheck={false} />
        {newThreadError && <p className="thread-create-error" role="alert">{newThreadError}</p>}
        <div className="thread-create-actions"><button className="secondary" type="button" onClick={closeNewThreadDialog} disabled={creatingThread}>取消</button><button className="primary" type="button" aria-label="创建会话" onClick={() => void createThread()} disabled={creatingThread || !newThreadCwd.trim()}>{creatingThread ? "正在创建…" : "创建"}</button></div>
      </section>
    </div>}
    {status !== "ready" && (phonePairing ? <PhonePairingPanel pairing={phonePairing} origin={window.location.origin} onConfirm={confirmPhonePairing} onCancel={cancelPhonePairing} /> : <section className="connect-panel" aria-label="连接 Relay"><div><div className="connect-hero"><ArielLogo size="clamp(128px, 12vw, 160px)" variant="color" decorative priority className="connect-logo" /><div className="connect-hero-copy"><span className="eyebrow">PRIVATE ACCESS</span><h1>接续 Codex</h1></div></div><p>输入 6 位连接码。连接后，同一标签页刷新会自动恢复；连接码不会存入浏览器。</p></div><form onSubmit={e => { e.preventDefault(); if (isWebPIN(token)) { savedSessionAttempt.current = false; setSessionExpired(false); client.connect(token); } }}><label htmlFor="token">6 位连接码</label><div className="connect-row"><input id="token" type="password" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={token} onChange={e => setToken(e.target.value)} autoComplete="off" placeholder="输入 6 位数字" required /><button className="primary" type="submit" disabled={!isWebPIN(token)}>连接 <span aria-hidden="true">↗</span></button></div>{status === "invalid" && <p role="alert" className="connect-error">{sessionExpired ? "保存的会话已失效，请重新输入连接码。" : "连接码错误或 Relay 已锁定；累计 10 次错误后需重启 Relay。"}</p>}<small>仅建议在可信局域网使用。HTTP/WS 连接未加密。</small></form></section>)}
    <div className="workspace">
      {showList && <button className="sidebar-backdrop" type="button" aria-label="关闭会话列表遮罩" onClick={() => setShowList(false)} />}
      <aside id="session-sidebar" className={`sidebar ${showList ? "open" : ""}`} aria-label="会话列表">
        <div className="sidebar-identity"><div className="brand"><ArielLogo size={29} variant="micro" tone="white" decorative className="brand-logo" /><span>Ariel</span></div><span className={`connection ${status}`}><span className="status-dot" />{connectionLabel}</span><button className="sidebar-disconnect text-button" aria-label="断开" onClick={disconnect}>断开</button></div>
        <div className="sidebar-head"><span className="eyebrow">WORKSPACE</span><h2>会话</h2>{threadCreateEnabled && <button className="icon-button" type="button" aria-label="新建会话" title="新建会话" onClick={openNewThreadDialog}>＋</button>}<button className="icon-button mobile-close" aria-label="关闭会话列表" onClick={() => setShowList(false)}>×</button><button className="icon-button" aria-label="刷新会话" onClick={() => void loadThreads(deviceId, "", searchInputRef.current.trim())} disabled={!deviceId || searchInput.trim() !== searchTerm}>↻</button></div>
        <label className="device-label" htmlFor="device">设备</label><select id="device" value={deviceId} onChange={e => setDeviceId(e.target.value)} disabled={status !== "ready"}><option value="">{devices.length ? "选择设备" : "暂无在线设备"}</option>{devices.map(d => <option key={d.deviceId} value={d.deviceId}>{d.deviceName}</option>)}</select>
        {device && <div className="device-meta"><span className={`status-dot ${device.agentOnline ? "online" : ""}`} />{device.agentOnline ? "Agent 在线" : "Agent 离线"}<span>·</span>{device.codexReady ? "Codex 就绪" : mock ? "Mock 演示" : "Codex 未就绪"}</div>}
        <div className="sidebar-search"><span className="search-glyph" aria-hidden="true">⌕</span><input type="search" aria-label="搜索会话" placeholder="搜索会话与消息" value={searchInput} maxLength={128} onChange={e => updateSearch(e.target.value)} disabled={status !== "ready" || !deviceId} />{searchInput && <button type="button" aria-label="清空搜索" onClick={() => updateSearch("")}>×</button>}</div>
        <div className="list-caption"><span>{searchInput.trim() ? "搜索结果" : "最近会话"}</span><span>{pinnedThreads.length > 0 ? `${pinnedThreads.length} 个置顶 · ` : ""}{threadGroups.length} 个项目 · {threads.length} 个会话</span></div>
        <div className="thread-list">{pinnedThreads.length > 0 && <section className="pinned-group" aria-label="置顶会话"><header className="pinned-heading"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M9 3h6l-1 6 3 3v2H7v-2l3-3-1-6Zm3 11v7" /></svg><span>置顶</span><span className="project-count" aria-hidden="true">{pinnedThreads.length}</span></header><div className="pinned-threads">{pinnedThreads.map(t => <button key={t.threadId} data-thread-id={t.threadId} className={`thread-row ${threadId === t.threadId ? "selected" : ""}`} onClick={() => void selectThread(t.threadId)}><span className="thread-title">{t.title || "未命名会话"}</span>{searchInput.trim() && t.searchSnippet && <span className="thread-snippet">{t.searchSnippet}</span>}<span className="thread-date">{new Date(t.updatedAt).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })}</span></button>)}</div></section>}{threadGroups.map((group, index) => {
          const expanded = expandedProjectKeys.has(group.key);
          const projectThreadsID = `project-threads-${index}`;
          return <section className={`project-group ${expanded ? "expanded" : "collapsed"}`} aria-label={`项目 ${group.name}`} key={group.key}>
            <header className="project-heading"><button className="project-toggle" type="button" aria-expanded={expanded} aria-controls={projectThreadsID} aria-label={`${expanded ? "收起" : "展开"}项目 ${group.name}，${group.path}`} onClick={() => toggleProjectGroup(group.key)}><span className={`project-chevron ${expanded ? "expanded" : ""}`} aria-hidden="true">›</span><span className="project-copy"><span className="project-name">{group.name}</span><span className="project-path" title={group.path}>{group.path}</span></span><span className="project-count" aria-hidden="true">{group.threads.length}</span></button></header>
            <div id={projectThreadsID} className="project-threads" hidden={!expanded}>{expanded && group.threads.map(t => <button key={t.threadId} data-thread-id={t.threadId} className={`thread-row ${threadId === t.threadId ? "selected" : ""}`} onClick={() => void selectThread(t.threadId)}><span className="thread-title">{t.title || "未命名会话"}</span>{searchInput.trim() && t.searchSnippet && <span className="thread-snippet">{t.searchSnippet}</span>}<span className="thread-date">{new Date(t.updatedAt).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })}</span></button>)}</div>
          </section>;
        })}{listLoading && <p className="list-state" role="status">{searchInput.trim() ? "正在搜索会话…" : "正在加载会话…"}</p>}{listError && <p className="list-state list-error" role="alert">{listError}</p>}{!listLoading && !listError && searchInput.trim() && threads.length === 0 && status === "ready" && <p className="list-state">没有找到匹配会话</p>}</div>
        {cursor && <button className="load-more" onClick={() => void loadThreads(deviceId, cursor, searchInputRef.current.trim())} disabled={listLoading}>加载更多 →</button>}
        <div className="sidebar-foot">{mock ? "MOCK SESSION · 非真实 Codex 历史" : "原始会话 · 不创建远程副本"}</div>
      </aside>
      <main className="conversation">
        <div className={`conversation-head ${historyChromeCollapsed ? "history-collapsed" : ""}`} data-history-collapsed={historyChromeCollapsed}>
          <button className="mobile-list text-button" aria-label={`打开会话列表，${connectionLabel}`} aria-expanded={showList} aria-controls="session-sidebar" onClick={() => setShowList(true)}><span className="menu-glyph" aria-hidden="true">☰</span><span className={`status-dot ${status === "ready" ? "online" : ""}`} aria-hidden="true" /></button>
          <div className="conversation-title"><span className="eyebrow">{mock ? "MOCK DEMO" : "CODEX SESSION"}</span><h2>{view ? view.thread.title || "未命名会话" : selectedThread ? selectedThread.title || "未命名会话" : "选择一个会话"}</h2><span className="head-path">{view?.thread.cwd || selectedThread?.cwd || "从左侧选择历史会话，接着工作。"}</span></div>
          <div className="head-right">{mock && <span className="mock-badge">模拟环境</span>}{view && <span className="runtime">{view.thread.runtime === "inProgress" ? "运行中" : view.thread.runtime === "idle" ? "待命" : view.thread.runtime === "notLoaded" ? "加载中" : "状态未知"}</span>}</div>
          {currentPermissions && <div className="permission-info" ref={permissionInfoRef}><button className={`permission-info-button ${permissionWarning ? "danger" : ""}`} type="button" aria-label={currentPermissions.label} aria-expanded={permissionInfoOpen} aria-controls="desktop-permission-details" onClick={() => setPermissionInfoOpen(open => !open)}><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 10.5v5"/><path d="M12 7.5h.01"/></svg></button>{permissionInfoOpen && <div id="desktop-permission-details" className="permission-popover" role="dialog" aria-label="当前 Desktop 权限详情"><div className="permission-popover-title">当前 Desktop 权限</div><div>{currentPermissions.label}</div>{currentPermissions.warning && <p>{currentPermissions.warning}</p>}</div>}</div>}
        </div>
        {currentPermissions && <section className={`permission-strip ${currentPermissions.warning ? "danger" : ""}`} aria-label="当前 Desktop 权限" role={currentPermissions.warning ? "alert" : "status"}><span>{currentPermissions.label}</span>{currentPermissions.warning && <span className="permission-note">{currentPermissions.warning}</span>}</section>}
        {readOnlyHistory && view && <div className="readonly-banner" role="status">历史只读 · 当前 Desktop 状态未确认，发送、停止和审批已禁用。</div>}
        <div className="transcript" ref={transcriptRef} onScroll={onTranscriptScroll} aria-live="polite">
          {!view && <div className="empty"><div className="empty-symbol"><ArielLogo size={50} variant="micro" tone="white" decorative className="session-loading-logo" /></div><h3>{blockedSelection.current === `${deviceId}\u0000${threadId}` ? "会话状态无法确认" : threadId ? "正在同步会话…" : "从这里接续"}</h3><p>{blockedSelection.current === `${deviceId}\u0000${threadId}` ? "远程操作已暂停。请稍后手动重新选择会话。" : threadId ? "等待电脑端加载原始历史。" : "选一个会话，历史、运行状态与需要你决定的问题会出现在这里。"}</p></div>}
          {view?.thread.historyComplete === false && !history.exhausted && <div className="history-control"><button className="history-action" type="button" onClick={() => void loadOlder()} disabled={historyLoading}>{historyLoading ? "正在加载更早消息…" : "加载更早消息"}</button></div>}
          {displayedTurns.slice(0, history.older.length).map(turn => <ConversationTurn key={`${view?.threadId}:${turn.turnId}`} turn={turn} loading={itemLoading === turn.turnId} onLoadOlderItems={turn => void loadOlderItems(turn)} onLoadImage={(itemId, index) => loadImage(turn.turnId, itemId, index)} active={status === "ready" && view?.thread.runtime === "inProgress" && view.thread.pendingInteractions.length === 0 && activeTurn?.turnId === turn.turnId} />)}
          {history.gap && <div className="history-gap">中间消息已从当前浏览窗口释放 <button type="button" onClick={resetHistory}>回到最新</button></div>}
          {displayedTurns.slice(history.older.length).map(turn => <ConversationTurn key={`${view?.threadId}:${turn.turnId}`} turn={turn} loading={itemLoading === turn.turnId} onLoadOlderItems={turn => void loadOlderItems(turn)} onLoadImage={(itemId, index) => loadImage(turn.turnId, itemId, index)} active={status === "ready" && view?.thread.runtime === "inProgress" && view.thread.pendingInteractions.length === 0 && activeTurn?.turnId === turn.turnId} />)}
          {view?.thread.pendingInteractions.map(card => <InteractionCard key={card.interactionId} card={card} values={answers[card.interactionId] || {}} onChange={(id, value) => setAnswers(all => ({ ...all, [card.interactionId]: { ...all[card.interactionId], [id]: value } }))} onRespond={decision => void respond(card, decision)} disabled={working} />)}
        </div>
        {(showReturnToLatest || historyChromeCollapsed) && view && <div className={`return-latest-bar ${historyChromeCollapsed ? "history-overlay" : ""}`}><button type="button" aria-label={showReturnToLatest ? "回到最新" : "恢复输入区并回到最新"} onClick={returnToLatest}>回到最新</button></div>}
        <div className={`composer-wrap ${historyChromeCollapsed ? "history-collapsed" : ""}`} data-history-collapsed={historyChromeCollapsed}><div className="composer-stack">{notice && <div className="notice" role="alert"><span>!</span>{notice}<button aria-label="关闭提示" onClick={() => setNotice("")}>×</button></div>}
          {queuedMessages.length > 0 && <section className="queue-panel" aria-label="排队的后续输入">
            {queuePaused && <div className="queue-head"><span>后续输入已暂停</span><small>开始下一轮前可继续调整</small></div>}
            <span className="sr-only">共 {queuedMessages.length} 条，最上方优先执行。</span>
            <ol className="queue-list">{renderedQueuedMessages.map((item, index) => {
              const busy = !!queueWorking || working || !!draggingQueueId;
              const summary = item.text.trim() || (item.images.length ? `${item.images.length} 张截图` : "无法预览的输入");
              return <li className={`queue-item ${editingQueueId === item.queueId ? "editing" : ""} ${draggingQueueId === item.queueId ? "dragging" : ""}`} key={item.queueId} data-queue-id={item.queueId}>
                <button className="queue-handle" type="button" aria-label={`拖拽排序：${summary}`} title="拖拽排序；也可用上下方向键" onPointerDown={event => beginQueueDrag(item, event)} onKeyDown={event => queueHandleKeyDown(index, event)} disabled={!!queueWorking || working}><QueueHandleIcon /></button>
                <div className="queue-content"><span className="queue-text">{summary}</span>{item.images.length > 0 && item.text.trim() && <small>{item.images.length} 张截图</small>}{!item.editable && <small>此输入只能删除或调序</small>}</div>
                <div className="queue-actions">
                  {item.editable && <button className="queue-guide-action" type="button" aria-label={`引导：${summary}`} title="插入当前思考，不中断 Codex" onClick={() => steerQueuedMessage(item)} disabled={busy || !activeTurn || !!view?.thread.pendingInteractions.length}><QueueGuideIcon /><span>引导</span></button>}
                  <button className="queue-icon-action" type="button" aria-label={`删除排队消息：${summary}`} title="删除" onClick={() => deleteQueuedMessage(item)} disabled={busy}><QueueTrashIcon /></button>
                  <div className="queue-more-wrap">
                    <button className="queue-more-action" type="button" aria-label={`更多选项：${summary}`} title="更多选项" aria-haspopup="menu" aria-expanded={queueMenuId === item.queueId} onClick={event => toggleQueueMenu(item, event)} disabled={busy}>•••</button>
                    {queueMenuId === item.queueId && createPortal(<div className="queue-menu" role="menu" aria-label={`排队消息选项：${summary}`} style={{ left: queueMenuPosition.left, top: queueMenuPosition.top, transform: queueMenuPosition.above ? "translateY(-100%)" : undefined }}>
                      {item.editable && <button type="button" role="menuitem" onClick={() => editQueuedMessage(item)}>编辑</button>}
                      <button type="button" role="menuitem" onClick={() => moveQueuedMessage(index, -1)} disabled={index === 0}>上移</button>
                      <button type="button" role="menuitem" onClick={() => moveQueuedMessage(index, 1)} disabled={index === renderedQueuedMessages.length - 1}>下移</button>
                    </div>, document.body)}
                  </div>
                </div>
              </li>;
            })}</ol>
          </section>}
          {screenshots.length > 0 && <div className="screenshot-drafts" aria-label="待发送截图">{screenshots.map((image, index) => <div className="screenshot-draft" key={`${image.name}:${index}`}><img src={image.dataUri} alt={image.name} /><button type="button" aria-label={`移除截图：${image.name}`} onClick={() => setScreenshots(current => current.filter((_, position) => position !== index))}>×</button></div>)}</div>}
          <div className="composer"><textarea ref={composerInputRef} aria-label="发送消息" enterKeyHint="enter" placeholder={view ? editingQueueId ? "编辑排队消息…" : view.thread.runtime === "inProgress" ? queueEnabled ? "继续输入，发送后加入队列…" : "Codex 正在运行；你可以先写草稿…" : "给 Codex 发消息…" : "选择会话后开始输入…"} value={draft} onChange={e => setDraft(e.target.value)} onKeyDown={e => { if (window.innerWidth > mobileViewportMaxWidth && e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(); } }} disabled={!view || status !== "ready" || readOnlyHistory} rows={1} />
            <div className="composer-actions"><span>Enter 发送 · Shift+Enter 换行</span><div>
              <input ref={screenshotInputRef} className="screenshot-file" type="file" aria-label="附加截图" accept="image/png,image/jpeg" multiple onChange={e => void addScreenshots(e.target.files)} disabled={!view || status !== "ready" || readOnlyHistory || working} />
              <button className="attach-button" type="button" aria-label="选择截图" title="附加截图" onClick={() => screenshotInputRef.current?.click()} disabled={!view || status !== "ready" || readOnlyHistory || working}>＋</button>
              {editingQueueId && <button className="queue-cancel-button" type="button" onClick={cancelQueueEdit} disabled={working}>取消编辑</button>}
              {activeTurn && <button className="stop-button" type="button" aria-label="停止" title="停止" onClick={() => void stop()} disabled={stopping || status !== "ready" || readOnlyHistory}><span className="stop-glyph" aria-hidden="true">■</span><span className="stop-label" aria-hidden="true">停止</span></button>}
              <button className="primary send-button" aria-label={sendAction} title={sendAction} onClick={() => void send()} disabled={readOnlyHistory || !canSend(view?.thread || null, status === "ready", working || !!queueWorking, draft, screenshots.length, queueEnabled)}><span className="send-label">{sendAction}</span><span className="send-glyph" aria-hidden="true">↑</span></button>
            </div></div></div></div></div>
      </main>
    </div>
    {pairingInvite && <PairingDialog invite={pairingInvite} seconds={pairingSeconds} onClose={closePairingInvite} onRegenerate={() => void openPairingInvite()} />}
  </div>;
}

function ConversationTurn({ turn, loading, onLoadOlderItems, onLoadImage, active }: { turn: Turn; loading: boolean; onLoadOlderItems: (turn: Turn) => void; onLoadImage: (itemId: string, index: number) => Promise<string>; active: boolean }) {
  const [activityExpanded, setActivityExpanded] = useState(false);
  const parts = groupTurnItems(turn);
  const lastPart = parts.at(-1);
  const processing = active && lastPart?.kind === "activity";
  const activityParts = parts.filter(part => part.kind === "activity");
  const activityCount = activityParts.reduce((count, part) => count + part.items.length, 0);
  const firstActivity = activityParts[0];
  const activityLabel = `${activityExpanded ? "收起" : "打开"}工具调用（${activityCount} 项）`;
  const activityToggleText = activityExpanded ? activityLabel : `使用了 ${activityCount} 个工具`;
  return <section className="turn">
    {turn.status !== "inProgress" && <div className="turn-status">{turn.status === "completed" ? "已完成" : turn.status === "interrupted" ? "已停止" : "失败"}</div>}
    {turn.itemsComplete === false && <button className="history-action item-history-action" type="button" onClick={() => onLoadOlderItems(turn)} disabled={loading}>{loading ? "正在加载…" : "加载此回合更早内容"}</button>}
    {parts.map(part => {
      if (part.kind === "message") {
        const item = part.item;
        const markdownImages = item.images?.filter(image => image.kind === "markdown") || [];
        const nativeImages = item.images?.filter(image => image.kind === "native") || [];
        return <article className={`message ${item.role}`} aria-label={item.role === "user" ? "你" : item.role === "assistant" ? "Codex" : "系统"} key={item.itemId} data-item-id={item.itemId}><div className="message-body"><div className="message-text">{item.role === "system" ? item.text : <ConversationMarkdown text={item.text} renderImage={(source, alt) => { const ref = markdownImages.find(image => image.source === source); return ref ? <ConversationImage alt={alt || ref.alt} autoLoad={false} load={() => onLoadImage(item.itemId, ref.index)} /> : null; }} />}</div>{nativeImages.map(image => <ConversationImage key={image.index} alt={image.alt} load={() => onLoadImage(item.itemId, image.index)} />)}</div></article>;
      }
      if (!activityExpanded && part !== firstActivity) return null;
      return <section className="activity-group" key={part.items[0].itemId} data-item-id={part === firstActivity ? `${turn.turnId}:activity-control` : undefined}>
        {part === firstActivity && <button className={`activity-toggle ${activityExpanded ? "open" : ""}`} type="button" aria-label={activityLabel} title={activityLabel} aria-expanded={activityExpanded} onClick={() => setActivityExpanded(open => !open)}>{activityToggleText}</button>}
        {activityExpanded && <div className="activity-list">{part.firstLoaded && <p className="activity-note">此回合还有更早内容，可在上方按需加载。</p>}{part.items.map(item => <div className="activity-item" key={item.itemId} data-item-id={item.itemId}><div className="activity-item-head"><span>{item.activity!.label}</span><span>{activityStatusText(item.activity!.status)}</span></div><div className="activity-kind">{item.activity!.kind}</div>{item.activity!.details ? <pre className="activity-details">{item.activity!.details}</pre> : <p className="activity-note">当前历史没有更多详情。</p>}{item.activity!.truncated && <p className="activity-note">详情已截断；完整内容请在原 Codex Desktop 查看。</p>}</div>)}</div>}
      </section>;
    })}
    {active && <div className="agent-progress" role="status">{processing ? "处理中…" : "思考中…"}</div>}
  </section>;
}

export function InteractionCard({ card, values, onChange, onRespond, disabled }: { card: Interaction; values: Record<string, string>; onChange: (id: string, value: string) => void; onRespond: (decision: string) => void; disabled: boolean }) {
  return <section className="interaction-card"><span className="eyebrow">NEEDS YOUR INPUT</span><h3>{card.kind === "command_approval" ? "等待命令审批" : card.kind === "file_approval" ? "等待文件变更审批" : card.kind === "permission_request" ? "等待权限请求" : card.kind === "user_input" ? "Codex 有一个问题" : "暂不支持的交互"}</h3><p>{card.prompt}</p>{card.questions?.map(q => <label key={q.id} className="question">{q.question}<input list={`options-${card.interactionId}-${q.id}`} value={values[q.id] || ""} onChange={e => onChange(q.id, e.target.value)} placeholder={q.options?.length ? "选择建议或自行输入" : "输入回答"} /><datalist id={`options-${card.interactionId}-${q.id}`}>{q.options?.map(o => <option key={o} value={o} />)}</datalist></label>)}<div className="interaction-actions">{card.availableDecisions.map(decision => <button key={decision} className={decision === "accept_once" || decision === "answer" ? "primary" : "secondary"} disabled={disabled || (decision === "answer" && !answersForSubmission(card, values))} onClick={() => onRespond(decision)}>{decision === "accept_once" && card.kind === "permission_request" ? "仅本轮按原请求授权" : ({ accept_once: "仅本次允许", deny: "拒绝", deny_and_stop: "拒绝并停止", answer: "提交回答" } as Record<string, string>)[decision]}</button>)}</div></section>;
}
