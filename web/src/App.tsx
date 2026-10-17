import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type TouchEvent as ReactTouchEvent, type WheelEvent as ReactWheelEvent } from "react";
import { createPortal } from "react-dom";
import { ArielSocket, isWebPIN, type ConnectionStatus } from "./client";
import { applyThreadEvent, belongsToSubscription, keepOfflineDevice, recoveryTarget, permissionSummary, canSend, type ThreadView } from "./state";
import { answersForSubmission } from "./interaction";
import { newRequestID } from "./ids";
import { WebSession } from "./session";
import { appendOlderPage, emptyHistoryState, prependOlderItems, type HistoryState } from "./history";
import { BrowserHistoryCache, mergeCachedHistory, type HistoryCacheIdentity } from "./history-cache";
import { DurableHistoryCache } from "./durable-history-cache";
import { BrowserThreadListCache, type ThreadListCacheIdentity } from "./thread-list-cache";
import { BrowserThreadUnreadStore } from "./thread-unread";
import { ComposerDraftStore, composerDraftKey, emptyComposerDraft, type ComposerDraftIdentity, type ComposerDraftSnapshot } from "./composer-drafts";
import { activityStatusText, groupTurnItems } from "./activity";
import { ConversationMarkdown } from "./markdown";
import { ConversationImage, readScreenshotFiles, type ScreenshotDraft } from "./screenshots";
import { isPairingCredential, pairingQRCode, pairingURL, takePairingCredential } from "./pairing";
import { groupThreadsByProject, mergeThreadPages, partitionThreadsByPin, unreadProjectKeys } from "./projects";
import { reorderQueueAtPointer } from "./queue-order";
import { ArielLogo } from "./ArielLogo";
import { loadAuthStatus, loginWithPasskey, logoutPasskey, registerPasskey } from "./auth";
import { FileContentCache, FileQuoteChips, FileViewer, HistoricalFileQuotes, TurnFileChanges, projectCanonicalFileContexts, webFileContextEnabled, type FileChange, type FileQuote, type FileView, type FileViewMode, type OpenFile } from "./file-context";
import type { ArielProtocolV1Envelope, Thread, Turn, Item, Response, Interaction, QueuedMessage } from "./generated/protocol";
import "./interaction.css";

type Device = { deviceId: string; deviceName: string; agentOnline: boolean; codexReady: boolean; agentEpoch?: string; adapterVersion?: string; capabilities: { autoLoad: boolean; history?: boolean; send?: boolean; interrupt?: boolean; interaction?: boolean; queue?: boolean; threadCreate?: boolean; threadBootstrap?: boolean; threadArchive?: boolean; threadArchiveUndo?: boolean; threadRename?: boolean; threadPin?: boolean; chillingFileContext?: boolean } };
const wsURL = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/ws`;
const recentTurnLimit = 4;
const historyTurnPageLimit = 10;
const mobileViewportMaxWidth = 800;
const composerMinHeight = 44;
const composerMaxHeight = 24 * 8 + 20; // Eight 24px lines plus vertical padding.
const latestFollowDistance = 80;
const historyHeaderExpandDistance = 32;
const latestChromeRestoreDuration = 260;
const transcriptGestureGraceMs = 500;
const agentOfflineGraceMs = 8_000;
const agentOfflineNotice = "Desktop Agent 暂时离线，恢复后会重新同步当前会话。";
const queueMenuWidth = 108;
const queueMenuRowHeight = 34;
type ReadingAnchor = { key: string; itemId: string; top: number; scrollTop: number; scrollHeight: number };
type PhonePairing = { credential: string; phase: "confirm" | "connecting" | "error" };
type PairingInvite = { status: "loading" | "waiting" | "consumed" | "expired" | "error"; credential: string; expiresAt: number; qr: string; message: string };
type QueueDragState = { pointerId: number; queueId: string; startY: number; order: string[]; cleanup: () => void };
type PendingSubmission = { deviceId: string; agentEpoch: string; threadId: string; clientMessageId: string; revision: number; queueId: string };
type ThreadMenuState = { threadId: string; left: number; top: number };
type ThreadLongPress = { threadId: string; pointerId: number; startX: number; startY: number; timer: number };
type ArchiveUndoToast = { threadId: string; title: string; operationId: string };
type RenameThreadDialog = { threadId: string; name: string };
type LocalThreadDraft = { deviceId: string; agentEpoch: string; cwd: string; draftId: string };
type WebAuthMode = "pin" | "passkey";
type PasskeyAuthState = { loaded: boolean; enrollmentRequired: boolean; error: string };

function webAuthModeFromDocument(): WebAuthMode {
  return document.querySelector<HTMLMetaElement>('meta[name="ariel-auth-mode"]')?.content === "passkey" ? "passkey" : "pin";
}

function readingKey(deviceId: string, threadId: string): string { return `${deviceId}\u0000${threadId}`; }

function validThreadName(name: string): boolean {
  const trimmed = name.trim();
  return trimmed.length > 0 && Array.from(trimmed).length <= 200 && new TextEncoder().encode(trimmed).length <= 800 && !/[\u0000-\u001f\u007f-\u009f]/u.test(trimmed);
}

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
const resultText: Record<string, string> = { DEVICE_OFFLINE: "设备离线，请确认电脑上的 Agent 已连接。", TURN_BUSY: "这个会话正在运行；草稿已保留，不会自动重发。", STALE_TURN: "运行中的 turn 已变化，请刷新状态后再停止。", STALE_INTERACTION: "这项交互已经变化或过期，请查看最新会话状态。", OUTCOME_UNKNOWN: "执行结果不确定。请先查看会话状态，不要直接重发。", RESYNC_REQUIRED: "事件顺序发生变化，正在重新同步。", NATIVE_STATE_UNCERTAIN: "Codex 原生会话状态暂时无法确认，已停止此会话的远程操作。请稍后手动重新选择；若持续出现，请在电脑端查看。", HISTORY_TOO_LARGE: "此页内容超过安全传输上限；已保留当前可见内容。", INTERACTION_UNSUPPORTED: "这张卡片已失效或当前决定不可用。", INVALID_ARGUMENT: "请求内容无效。", OVERLOADED: "请求过多，请稍后再试。", PROTOCOL_UNSUPPORTED: "当前 Codex 版本不支持这项操作。", TIMEOUT: "操作超时，请重试。", FEATURE_DISABLED: "文件引用功能尚未启用。", CHILLING_UNAVAILABLE: "本机 Chilling 暂不可用。", CONTRACT_UNSUPPORTED: "Chilling 文件协议版本不兼容。", FILE_NOT_FOUND: "文件已不存在。", PATH_NOT_ALLOWED: "该路径不允许读取。", UNSUPPORTED_FILE_TYPE: "当前只支持 UTF-8 文本文件。", FILE_TOO_LARGE: "文件超过 1 MiB，无法在这里打开。", FILE_CHANGED: "文件已经变化，请重新打开并选择片段。" };

function errorText(response: Response): string {
  return response.error ? resultText[response.error.code] || response.error.message : "操作未完成。";
}

function imageErrorText(response: Response): string {
  switch (response.error?.code) {
    case "NOT_FOUND": return "原图片文件已不存在";
    case "INVALID_ARGUMENT": return "图片格式、尺寸或内容不受支持";
    case "HISTORY_TOO_LARGE": return "图片超过安全传输上限";
    default: return response.error ? resultText[response.error.code] || "图片暂时无法读取" : "图片暂时无法读取";
  }
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

function ReturnToLatestIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M12 4v16"/><path d="m6 14 6 6 6-6"/></svg>;
}

function MenuIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="M5 7h14M5 12h14M5 17h14"/></svg>;
}

function CloseIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="m6 6 12 12M18 6 6 18"/></svg>;
}

function PlusIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="M12 5v14M5 12h14"/></svg>;
}

function RefreshIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M19 8a8 8 0 1 0 1 6"/><path d="M19 4v4h-4"/></svg>;
}

function SearchIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><circle cx="11" cy="11" r="6"/><path d="m16 16 4 4"/></svg>;
}

function PairIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="M4 9V5a1 1 0 0 1 1-1h4M15 4h4a1 1 0 0 1 1 1v4M20 15v4a1 1 0 0 1-1 1h-4M9 20H5a1 1 0 0 1-1-1v-4"/><path d="M9 9h6v6H9z"/></svg>;
}

function PasskeyIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><circle cx="8" cy="12" r="4"/><path d="M12 12h8M17 12v3M20 12v2"/></svg>;
}

function DisconnectIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="M12 3v9"/><path d="M6.3 6.6a8 8 0 1 0 11.4 0"/></svg>;
}

function MoreIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="1.5"/><circle cx="12" cy="12" r="1.5"/><circle cx="19" cy="12" r="1.5"/></svg>;
}

function StopIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="currentColor"><rect x="8" y="8" width="8" height="8" rx="1"/></svg>;
}

function SendIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round"><path d="M12 19V5"/><path d="m6.5 10.5 5.5-5.5 5.5 5.5"/></svg>;
}

function SendLoadingIcon() {
  return <svg className="send-loading-spinner" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M20 12a8 8 0 1 1-2.34-5.66"/></svg>;
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
      <button className="pairing-close icon-button icon-control" type="button" aria-label="关闭扫码登录" data-tooltip="关闭" onClick={onClose}><CloseIcon /></button>
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

export function App({ initialPairingCredential, durableHistoryCache }: { initialPairingCredential?: string; durableHistoryCache?: DurableHistoryCache } = {}) {
  const client = useMemo(() => new ArielSocket(wsURL), []);
  const webSession = useMemo(() => new WebSession(() => window.sessionStorage), []);
  const durableCache = useMemo(() => durableHistoryCache ?? new DurableHistoryCache(), [durableHistoryCache]);
  const [webAuthMode] = useState<WebAuthMode>(webAuthModeFromDocument);
  const [passkeyAuth, setPasskeyAuth] = useState<PasskeyAuthState>({ loaded: false, enrollmentRequired: false, error: "" });
  const [passkeyBusy, setPasskeyBusy] = useState(false);
  const [setupToken, setSetupToken] = useState("");
  const [phonePairing, setPhonePairing] = useState<PhonePairing | null>(() => {
    const credential = initialPairingCredential ?? takePairingCredential(window.location, window.history);
    if (webAuthModeFromDocument() !== "pin") return null;
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
  const [unreadThreadIDs, setUnreadThreadIDs] = useState<Set<string>>(() => new Set());
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
  const [localThreadDraft, setLocalThreadDraft] = useState<LocalThreadDraft | null>(null);
  const searchInputRef = useRef("");
  const searchInputChanged = useRef(false);
  const [threadId, setThreadId] = useState("");
  const [view, setView] = useState<ThreadView | null>(null);
  const [readOnlyHistory, setReadOnlyHistory] = useState(false);
  const [syncingView, setSyncingView] = useState(false);
  const [history, setHistory] = useState<HistoryState>(emptyHistoryState);
  const historyRef = useRef<HistoryState>(emptyHistoryState());
  const [historyLoading, setHistoryLoading] = useState(false);
  const historyLoadingRef = useRef(false);
  const [itemLoading, setItemLoading] = useState("");
  const itemLoadingRef = useRef("");
  const [itemOverrides, setItemOverrides] = useState<Record<string, Turn>>({});
  const itemOverridesRef = useRef<Record<string, Turn>>({});
  const [draft, setDraft] = useState("");
  const [screenshots, setScreenshots] = useState<ScreenshotDraft[]>([]);
	const [fileQuotes, setFileQuotes] = useState<FileQuote[]>([]);
	const [editingQueueId, setEditingQueueId] = useState("");
	const [fileChanges, setFileChanges] = useState<Record<string, FileChange[]>>({});
	const [openFile, setOpenFile] = useState<FileView | null>(null);
	const openFileGeneration = useRef(0);
	const fileChangeRequests = useRef(new Set<string>());
	const fileContentCache = useRef(new FileContentCache());
	const fileContentCacheEpoch = useRef("");
  const composerSnapshot = useRef<ComposerDraftSnapshot>(emptyComposerDraft());
  const composerDrafts = useRef(new ComposerDraftStore());
  const activeComposerIdentity = useRef<ComposerDraftIdentity | null>(null);
  const pendingSubmissions = useRef(new Map<string, PendingSubmission>());
  const screenshotInputRef = useRef<HTMLInputElement>(null);
  const [notice, setNotice] = useState("");
  const [working, setWorking] = useState(false);
  const [queueWorking, setQueueWorking] = useState("");
  const [queueMenuId, setQueueMenuId] = useState("");
  const [queueMenuPosition, setQueueMenuPosition] = useState({ left: 0, top: 0, above: false });
  const [threadMenu, setThreadMenu] = useState<ThreadMenuState | null>(null);
  const [threadArchiveWorking, setThreadArchiveWorking] = useState("");
  const [threadMetadataWorking, setThreadMetadataWorking] = useState("");
  const [renameThreadDialog, setRenameThreadDialog] = useState<RenameThreadDialog | null>(null);
  const [renameThreadError, setRenameThreadError] = useState("");
  const [archiveUndoToast, setArchiveUndoToast] = useState<ArchiveUndoToast | null>(null);
  const [draggingQueueId, setDraggingQueueId] = useState("");
  const [dragQueueOrder, setDragQueueOrder] = useState<string[]>([]);
  const [stopping, setStopping] = useState(false);
  const [recoveringDeviceId, setRecoveringDeviceId] = useState("");
  const [showList, setShowList] = useState(true);
  const [showReturnToLatest, setShowReturnToLatest] = useState(false);
  const [historyChromeCollapsed, setHistoryChromeCollapsed] = useState(false);
  const [permissionInfoOpen, setPermissionInfoOpen] = useState(false);
  const [visualViewportHeight, setVisualViewportHeight] = useState<number | null>(null);
  const [answers, setAnswers] = useState<Record<string, Record<string, string>>>({});
  const selection = useRef({ deviceId: "", threadId: "", view: null as ThreadView | null });
  const devicesRef = useRef<Device[]>([]);
  const historyCache = useRef(new BrowserHistoryCache());
  const threadListCache = useRef(new BrowserThreadListCache());
  const warmSubscriptions = useRef(new Map<string, ThreadView>());
  const unreadStore = useMemo(() => new BrowserThreadUnreadStore(window.localStorage), []);
  const backgroundThreadListRefresh = useRef(false);
  const threadListRequestsInFlight = useRef(0);
  const restoredHistoryCache = useRef<HistoryCacheIdentity | null>(null);
  const expectedSubscription = useRef("");
  const blockedSelection = useRef("");
  const pendingSelect = useRef(0);
  // Spec 0086 progressive open: the most recent thread.read fast path, so a
  // failed or oversized owner sync can reuse it instead of re-requesting.
  const fastReadCache = useRef<{ deviceId: string; threadId: string; epoch: number; settled: boolean; thread: Thread | null; result: Promise<Thread | null> } | null>(null);
  const deviceListGeneration = useRef(0);
  const threadListGeneration = useRef(0);
  const threadCreateGeneration = useRef(0);
  const offlineDevice = useRef("");
  const offlineDeviceTimer = useRef<number | null>(null);
  const observedAgentEpochs = useRef(new Map<string, string>());
  const recoveryGeneration = useRef(0);
  const recoveryRunning = useRef(false);
  const connectionResume = useRef(false);
  const durableRestoreAttempt = useRef("");
  const transcriptRef = useRef<HTMLDivElement>(null);
  const followLatestRef = useRef(true);
  const lastTranscriptScrollTop = useRef(0);
  const historyReturnIntent = useRef(false);
  const transcriptTouchY = useRef<number | null>(null);
  const transcriptGestureDeadline = useRef(0);
  const latestChromeRestoreDeadline = useRef(0);
  const latestChromeRestoreTimer = useRef<number | null>(null);
  const pendingReadingAnchor = useRef<ReadingAnchor | null>(null);
  const resumeReadingAnchor = useRef<ReadingAnchor | null>(null);
  const lastPaint = useRef<{ key: string; seq: number; marker: ContentMarker | null }>({ key: "", seq: -1, marker: null });
  const permissionInfoRef = useRef<HTMLDivElement>(null);
  const composerInputRef = useRef<HTMLTextAreaElement>(null);
  const pairingGeneration = useRef(0);
  const queueDrag = useRef<QueueDragState | null>(null);
  const threadLongPress = useRef<ThreadLongPress | null>(null);
  const suppressThreadClick = useRef({ threadId: "", until: 0 });
  const threadMenuFirstItem = useRef<HTMLButtonElement>(null);
  const renameThreadInput = useRef<HTMLInputElement>(null);
  const device = devices.find(d => d.deviceId === deviceId);
  const selectedThread = threads.find(t => t.threadId === threadId);
  const allThreadGroups = useMemo(() => groupThreadsByProject(threads), [threads]);
  const { pinned: pinnedThreads, regular: regularThreads } = useMemo(() => partitionThreadsByPin(threads), [threads]);
  const threadGroups = useMemo(() => groupThreadsByProject(regularThreads), [regularThreads]);
  const unreadProjectIDs = useMemo(() => unreadProjectKeys(threads, unreadThreadIDs), [threads, unreadThreadIDs]);
  const mock = device?.adapterVersion?.startsWith("mock-") ?? false;
  const activeTurn = view?.thread.turns.findLast(t => t.status === "inProgress");
  const queuedMessages = view?.thread.queuedMessages || [];
  const queueEnabled = device?.capabilities.queue === true;
  const threadCreateEnabled = device?.capabilities.threadBootstrap === true;
  const threadArchiveEnabled = device?.capabilities.threadArchive === true;
  const threadRenameEnabled = device?.capabilities.threadRename === true;
  const threadPinEnabled = device?.capabilities.threadPin === true;
	const fileContextEnabled = webFileContextEnabled() && device?.capabilities.chillingFileContext === true;
  const threadContextActionsEnabled = threadArchiveEnabled || threadRenameEnabled || threadPinEnabled;
  const agentReady = status === "ready" && Boolean(device?.agentOnline && device.codexReady) && recoveringDeviceId !== deviceId;
  const ownerReady = agentReady && !syncingView;
  const composerReady = localThreadDraft ? agentReady : ownerReady;
  const ownerRecoveryLoading = Boolean(view && !readOnlyHistory && !ownerReady && (syncingView || recoveringDeviceId === deviceId));
  const queuePaused = queuedMessages.length > 0 && view?.thread.runtime === "idle" && view.thread.turns.at(-1)?.status === "interrupted";
  const sendAction = editingQueueId ? "保存排队消息" : view?.thread.runtime === "inProgress" && queueEnabled ? "加入队列" : "发送";
  const displayedSendAction = ownerRecoveryLoading ? "正在同步会话，暂不可发送" : sendAction;
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

  useEffect(() => {
    const previous = composerSnapshot.current;
    if (previous.draft === draft && previous.screenshots === screenshots && previous.fileQuotes === fileQuotes && previous.editingQueueId === editingQueueId) return;
    const next = { draft, screenshots, fileQuotes, editingQueueId, revision: previous.revision + 1 };
    composerSnapshot.current = next;
    if (activeComposerIdentity.current) composerDrafts.current.set(activeComposerIdentity.current, next);
  }, [draft, screenshots, fileQuotes, editingQueueId]);

	useEffect(() => {
		const epoch = deviceId ? `${deviceId}\u0000${device?.agentEpoch || ""}` : "";
		if (fileContentCacheEpoch.current && fileContentCacheEpoch.current !== epoch) {
			fileContentCache.current.clear();
			openFileGeneration.current++;
			setOpenFile(null);
		}
		fileContentCacheEpoch.current = epoch;
	}, [device?.agentEpoch, deviceId]);

	useEffect(() => {
		if (!fileContextEnabled || !view || !deviceId) return;
		const completed = displayedTurns.filter(turn => turn.status === "completed").slice(-recentTurnLimit);
		const pending = completed.filter(turn => {
			const key = `${deviceId}\u0000${device?.agentEpoch || ""}\u0000${view.threadId}\u0000${turn.turnId}`;
			return fileChanges[`${view.threadId}\u0000${turn.turnId}`] === undefined && !fileChangeRequests.current.has(key);
		});
		if (!pending.length) return;
		const requestKey = `${deviceId}\u0000${device?.agentEpoch || ""}\u0000${view.threadId}\u0000${pending.map(turn => turn.turnId).join(",")}`;
		fileChangeRequests.current.add(requestKey);
		for (const turn of pending) fileChangeRequests.current.add(`${deviceId}\u0000${device?.agentEpoch || ""}\u0000${view.threadId}\u0000${turn.turnId}`);
		void client.request("file.context.changes", deviceId, { threadId:view.threadId, turnIds:pending.map(turn=>turn.turnId) }).then(response => {
			if (selection.current.deviceId !== deviceId || selection.current.threadId !== view.threadId) return;
			if (response.outcome !== "accepted" || !Array.isArray(response.data?.turns)) {
				const unavailable = Object.fromEntries(pending.map(turn=>[`${view.threadId}\u0000${turn.turnId}`,[] as FileChange[]]));
				setFileChanges(current=>({...current,...unavailable}));
				return;
			}
			const next: Record<string,FileChange[]> = {};
			for (const raw of response.data.turns as unknown[]) {
				if (!raw || typeof raw !== "object") continue;
				const value = raw as {turnId?:unknown;fileChanges?:unknown}; if (typeof value.turnId !== "string" || !Array.isArray(value.fileChanges)) continue;
				const changes = value.fileChanges.filter((entry): entry is FileChange => !!entry && typeof entry === "object" && typeof (entry as FileChange).itemId === "string" && typeof (entry as FileChange).path === "string" && ["add","update","delete"].includes((entry as FileChange).kind));
				next[`${view.threadId}\u0000${value.turnId}`] = changes;
			}
			setFileChanges(current=>({...current,...next}));
		});
	}, [client, device?.agentEpoch, deviceId, fileChanges, fileContextEnabled, view?.threadId, displayedTurns.map(turn=>`${turn.turnId}:${turn.status}`).join("|")]);

	async function openChangedFile(turnId: string, change: FileChange, mode:FileViewMode) {
		if (!fileContextEnabled || !view || !deviceId || (mode === "source" && change.kind === "delete") || (mode === "diff" && !change.diff)) return;
		const generation=++openFileGeneration.current;
		const base:FileView={threadId:view.threadId,turnId,change,mode};
		if(mode==="diff"){
			setOpenFile(current=>current?.threadId===base.threadId&&current.turnId===base.turnId&&current.change.itemId===change.itemId&&current.change.path===change.path?{...base,file:current.file}:base);
			return;
		}
		const cacheKey = `${deviceId}\u0000${device?.agentEpoch || ""}\u0000${view.threadId}\u0000${turnId}\u0000${change.itemId}\u0000${change.path}`;
		const cached = fileContentCache.current.get(cacheKey);
		if (cached) { setOpenFile({...base,file:cached}); return; }
		setOpenFile(current=>current?.threadId===base.threadId&&current.turnId===base.turnId&&current.change.itemId===change.itemId&&current.change.path===change.path?{...base,file:current.file,loading:true}:{...base,loading:true});
		const response = await client.request("file.context.read",deviceId,{threadId:view.threadId,turnId,itemId:change.itemId,path:change.path});
		if(generation!==openFileGeneration.current)return;
		if (response.outcome !== "accepted") { setOpenFile(null);setNotice(errorText(response)); return; }
		const value=response.data as Partial<OpenFile> | undefined;
		if (!value || typeof value.content!=="string" || typeof value.sha256!=="string" || typeof value.modifiedAtMs!=="number" || typeof value.lineCount!=="number" || typeof value.language!=="string" || value.version!=="current-workspace") { setOpenFile(null);setNotice("文件响应格式无法识别。"); return; }
		const file:OpenFile={threadId:view.threadId,turnId,itemId:change.itemId,path:change.path,content:value.content,sha256:value.sha256,modifiedAtMs:value.modifiedAtMs,lineCount:value.lineCount,language:value.language,version:"current-workspace"};
		fileContentCache.current.set(cacheKey,file);
		setOpenFile({...base,file});
	}

	function closeFile(){openFileGeneration.current++;setOpenFile(null);}
	function changeFileMode(mode:FileViewMode){if(!openFile)return;void openChangedFile(openFile.turnId,openFile.change,mode);}

  function agentEpochFor(id: string): string {
    return devicesRef.current.find(candidate => candidate.deviceId === id)?.agentEpoch || "";
  }

  function warmSubscriptionKey(targetDevice: string, targetThread: string): string {
    const epoch = agentEpochFor(targetDevice);
    return targetDevice && epoch && targetThread ? `${targetDevice}\u0000${epoch}\u0000${targetThread}` : "";
  }

  function warmSubscription(targetDevice: string, targetThread: string): ThreadView | null {
    const key = warmSubscriptionKey(targetDevice, targetThread);
    return key ? warmSubscriptions.current.get(key) || null : null;
  }

  function retainWarmSubscription(next: ThreadView) {
    if (!next.subscriptionId || !next.streamId) return;
    const key = warmSubscriptionKey(next.deviceId, next.threadId);
    if (!key) return;
    warmSubscriptions.current.delete(key);
    warmSubscriptions.current.set(key, next);
  }

  function dropWarmSubscription(targetDevice: string, targetThread: string, unsubscribe = false) {
    const key = warmSubscriptionKey(targetDevice, targetThread);
    const existing = key ? warmSubscriptions.current.get(key) : null;
    if (key) warmSubscriptions.current.delete(key);
    if (unsubscribe && existing?.subscriptionId) void client.request("thread.unsubscribe", existing.deviceId, { subscriptionId: existing.subscriptionId });
  }

  function clearWarmSubscriptions(targetDevice = "", unsubscribe = false) {
    for (const [key, existing] of warmSubscriptions.current) {
      if (targetDevice && existing.deviceId !== targetDevice) continue;
      warmSubscriptions.current.delete(key);
      if (unsubscribe && existing.subscriptionId) void client.request("thread.unsubscribe", existing.deviceId, { subscriptionId: existing.subscriptionId });
    }
  }

  function pruneWarmSubscriptions(keep: ReadonlySet<string>) {
    for (const [key, existing] of warmSubscriptions.current) {
      if (keep.has(key)) continue;
      warmSubscriptions.current.delete(key);
      void client.request("thread.unsubscribe", existing.deviceId, { subscriptionId: existing.subscriptionId });
    }
  }

  function captureComposerSnapshot(): ComposerDraftSnapshot {
    const previous = composerSnapshot.current;
    if (previous.draft === draft && previous.screenshots === screenshots && previous.fileQuotes === fileQuotes && previous.editingQueueId === editingQueueId) return previous;
    const next = { draft, screenshots, fileQuotes, editingQueueId, revision: previous.revision + 1 };
    composerSnapshot.current = next;
    return next;
  }

  function showComposerSnapshot(snapshot: ComposerDraftSnapshot) {
    composerSnapshot.current = snapshot;
    setDraft(snapshot.draft);
    setScreenshots(snapshot.screenshots);
    setFileQuotes(snapshot.fileQuotes);
    setEditingQueueId(snapshot.editingQueueId);
  }

  function activateComposer(identity: ComposerDraftIdentity) {
    const currentIdentity = activeComposerIdentity.current;
    if (currentIdentity && composerDraftKey(currentIdentity) === composerDraftKey(identity)) return;
    if (currentIdentity) composerDrafts.current.set(currentIdentity, captureComposerSnapshot());
    const next = composerDrafts.current.get(identity) || emptyComposerDraft();
    activeComposerIdentity.current = identity;
    showComposerSnapshot(next);
  }

  function clearAllComposerDrafts() {
    composerDrafts.current.clear();
    pendingSubmissions.current.clear();
    activeComposerIdentity.current = null;
    showComposerSnapshot(emptyComposerDraft());
  }

  function sameSubmissionTarget(submission: PendingSubmission): boolean {
    return selection.current.deviceId === submission.deviceId
      && selection.current.threadId === submission.threadId
      && agentEpochFor(submission.deviceId) === submission.agentEpoch;
  }

  function clearSubmittedComposer(submission: PendingSubmission): boolean {
    const identity = { deviceId: submission.deviceId, threadId: submission.threadId };
    const active = activeComposerIdentity.current;
    if (active && composerDraftKey(active) === composerDraftKey(identity)) {
      const current = composerSnapshot.current;
      composerDrafts.current.set(identity, current);
      if (current.revision !== submission.revision) return false;
      const cleared = emptyComposerDraft(current.revision + 1);
      composerDrafts.current.set(identity, cleared);
      showComposerSnapshot(cleared);
      return true;
    }
    return composerDrafts.current.clearIfRevision(identity, submission.revision);
  }

  function reconcileSubmission(thread: Thread, eventDeviceId: string, eventThreadId: string) {
    for (const [clientMessageId, submission] of pendingSubmissions.current) {
      if (submission.deviceId !== eventDeviceId || submission.threadId !== eventThreadId) continue;
      const observed = thread.turns.some(turn => turn.items.some(item => item.role === "user" && item.clientMessageId === clientMessageId))
        || (thread.queuedMessages || []).some(item => item.clientMessageId === clientMessageId);
      if (!observed) continue;
      pendingSubmissions.current.delete(clientMessageId);
      if (clearSubmittedComposer(submission) && sameSubmissionTarget(submission)) setNotice("消息已确认发送。");
    }
  }

  function resetHistory() {
    const empty = emptyHistoryState();
    historyRef.current = empty;
    historyLoadingRef.current = false;
    setHistory(empty);
    setHistoryLoading(false);
    setItemLoading("");
    itemLoadingRef.current = "";
    itemOverridesRef.current = {};
    setItemOverrides({});
    restoredHistoryCache.current = null;
  }

  function clearAgentOfflineTimer() {
    if (offlineDeviceTimer.current !== null) window.clearTimeout(offlineDeviceTimer.current);
    offlineDeviceTimer.current = null;
  }

  function finishAgentRecovery(id: string) {
    if (offlineDevice.current !== id) return;
    clearAgentOfflineTimer();
    offlineDevice.current = "";
    setRecoveringDeviceId(current => current === id ? "" : current);
    setNotice(current => current === agentOfflineNotice ? "" : current);
  }

  function beginAgentOfflineGrace(id: string) {
    if (offlineDevice.current === id) return;
    clearAgentOfflineTimer();
    offlineDevice.current = id;
    pendingSelect.current++;
    expectedSubscription.current = "";
    setRecoveringDeviceId(id);
    setSyncingView(true);
    setNotice(current => current === agentOfflineNotice ? "" : current);
    offlineDeviceTimer.current = window.setTimeout(() => {
      if (offlineDevice.current !== id || selection.current.deviceId !== id) return;
      offlineDeviceTimer.current = null;
      pendingSelect.current++;
      expectedSubscription.current = "";
      selection.current.view = null;
      setView(null);
      setSyncingView(false);
      setReadOnlyHistory(false);
      setNotice(agentOfflineNotice);
    }, agentOfflineGraceMs);
  }

  function updateDevicePresence(event: Extract<ArielProtocolV1Envelope, { event: "device.status" }>) {
    const next = devicesRef.current.map(candidate => candidate.deviceId === event.deviceId
      ? { ...candidate, agentOnline: event.agentOnline, codexReady: event.codexReady, capabilities: event.capabilities || candidate.capabilities }
      : candidate);
    devicesRef.current = next;
    setDevices(next);
  }

  function replaceItemOverrides(next: Record<string, Turn>) {
    itemOverridesRef.current = next;
    setItemOverrides(next);
  }

  function historyCacheIdentity(targetDevice: string, targetThread: string): HistoryCacheIdentity | null {
    const agentEpoch = devicesRef.current.find(candidate => candidate.deviceId === targetDevice)?.agentEpoch;
    return agentEpoch ? { deviceId: targetDevice, agentEpoch, threadId: targetThread } : null;
  }

  function cacheCurrentHistory() {
    const current = selection.current;
    if (!current.view?.subscriptionId || current.view.deviceId !== current.deviceId || current.view.threadId !== current.threadId) return;
    const identity = historyCacheIdentity(current.deviceId, current.threadId);
    if (!identity) return;
    const value = { history: historyRef.current, itemOverrides: itemOverridesRef.current, thread: current.view.thread };
    historyCache.current.set(identity, value);
    void durableCache.set(identity, value);
    void durableCache.rememberSelection(identity);
  }

  function cacheWarmThread(next: ThreadView) {
    const identity = historyCacheIdentity(next.deviceId, next.threadId);
    const cached = identity ? historyCache.current.get(identity) : null;
    if (!identity || !cached) return;
    const value = { ...cached, thread: next.thread };
    historyCache.current.set(identity, value);
    void durableCache.set(identity, value);
  }

  function persistView(next: ThreadView) {
    const identity = historyCacheIdentity(next.deviceId, next.threadId);
    if (!identity) return;
    const value = { history: historyRef.current, itemOverrides: itemOverridesRef.current, thread: next.thread };
    historyCache.current.set(identity, value);
    void durableCache.set(identity, value);
    void durableCache.rememberSelection(identity);
  }

  function restoreCachedConversation(targetDevice: string, targetThread: string): ThreadView | null {
    const identity = historyCacheIdentity(targetDevice, targetThread);
    const cached = identity ? historyCache.current.get(identity) : null;
    if (!identity || !cached?.thread || cached.thread.threadId !== targetThread) return null;
    historyRef.current = cached.history;
    setHistory(cached.history);
    replaceItemOverrides(cached.itemOverrides);
    restoredHistoryCache.current = identity;
    return { deviceId: targetDevice, threadId: targetThread, subscriptionId: "", streamId: "", seq: 0, thread: cached.thread };
  }

  function restoreCachedHistory(targetDevice: string, targetThread: string, liveTurns: Turn[]): boolean {
    const identity = historyCacheIdentity(targetDevice, targetThread);
    const cached = identity ? historyCache.current.get(identity) : null;
    if (!identity || !cached) return false;
    const restored = mergeCachedHistory(cached, liveTurns);
    if (restored.history.older.length === 0) return false;
    historyRef.current = restored.history;
    setHistory(restored.history);
    replaceItemOverrides(restored.itemOverrides);
    restoredHistoryCache.current = identity;
    return true;
  }

  function discardRestoredHistory(targetDevice: string, targetThread: string): boolean {
    const restored = restoredHistoryCache.current;
    if (restored?.deviceId !== targetDevice || restored.threadId !== targetThread) return false;
    historyCache.current.delete(restored);
    void durableCache.delete(restored);
    const empty = emptyHistoryState();
    historyRef.current = empty;
    setHistory(empty);
    replaceItemOverrides({});
    restoredHistoryCache.current = null;
    return true;
  }

  function invalidateOwnerView(targetDevice: string, message: string) {
    if (selection.current.deviceId !== targetDevice) return;
    rememberReadingPosition();
    pendingSelect.current++;
    expectedSubscription.current = "";
    selection.current.view = null;
    setView(null);
    setSyncingView(false);
    setReadOnlyHistory(false);
    resetHistory();
    setNotice(message);
  }

  function rememberReadingPosition() {
    if (!selection.current.deviceId || !selection.current.threadId || followLatestRef.current) return;
    const anchor = captureReadingAnchor(transcriptRef.current, readingKey(selection.current.deviceId, selection.current.threadId));
    if (anchor) resumeReadingAnchor.current = anchor;
  }

  function cancelLatestChromeRestore() {
    latestChromeRestoreDeadline.current = 0;
    if (latestChromeRestoreTimer.current !== null) window.clearTimeout(latestChromeRestoreTimer.current);
    latestChromeRestoreTimer.current = null;
    historyReturnIntent.current = false;
  }

  function pinLatestUntilChromeSettles() {
    const container = transcriptRef.current;
    if (!container || !latestChromeRestoreDeadline.current) { cancelLatestChromeRestore(); return; }
    scrollToLatest(container);
    lastTranscriptScrollTop.current = container.scrollTop;
    if (performance.now() >= latestChromeRestoreDeadline.current) { cancelLatestChromeRestore(); return; }
    latestChromeRestoreTimer.current = window.setTimeout(pinLatestUntilChromeSettles, 16);
  }

  function restoreLatestChrome(container = transcriptRef.current) {
    const stabilizeLayout = historyChromeCollapsed || latestChromeRestoreDeadline.current > 0;
    cancelLatestChromeRestore();
    followLatestRef.current = true;
    historyReturnIntent.current = stabilizeLayout;
    transcriptTouchY.current = null;
    setShowReturnToLatest(false);
    if (stabilizeLayout) latestChromeRestoreDeadline.current = performance.now() + latestChromeRestoreDuration;
    setHistoryChromeCollapsed(false);
    if (container) {
      scrollToLatest(container);
      lastTranscriptScrollTop.current = container.scrollTop;
    }
    if (stabilizeLayout) latestChromeRestoreTimer.current = window.setTimeout(pinLatestUntilChromeSettles, 0);
  }

  function onTranscriptScroll() {
    const container = transcriptRef.current;
    if (!container) return;
    const composerEditingWithoutTranscriptGesture = window.innerWidth <= mobileViewportMaxWidth
      && document.activeElement === composerInputRef.current
      && followLatestRef.current
      && transcriptTouchY.current === null
      && performance.now() >= transcriptGestureDeadline.current;
    if (composerEditingWithoutTranscriptGesture) {
      scrollToLatest(container);
      lastTranscriptScrollTop.current = container.scrollTop;
      setShowReturnToLatest(false);
      return;
    }
    if (latestChromeRestoreDeadline.current) {
      const userScrolledUp = !historyReturnIntent.current && container.scrollTop < lastTranscriptScrollTop.current - 1;
      if (!userScrolledUp) {
        scrollToLatest(container);
        lastTranscriptScrollTop.current = container.scrollTop;
        followLatestRef.current = true;
        setShowReturnToLatest(false);
        return;
      }
      cancelLatestChromeRestore();
    }
    const scrollDelta = container.scrollTop - lastTranscriptScrollTop.current;
    if (scrollDelta > 0) historyReturnIntent.current = true;
    else if (scrollDelta < 0) historyReturnIntent.current = false;
    lastTranscriptScrollTop.current = container.scrollTop;
    const distanceFromLatest = Math.max(0, container.scrollHeight - container.clientHeight - container.scrollTop);
    if (historyChromeCollapsed) {
      const returnedToLatest = historyReturnIntent.current && distanceFromLatest <= historyHeaderExpandDistance;
      followLatestRef.current = returnedToLatest;
      if (returnedToLatest) {
        restoreLatestChrome(container);
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

  function applyHistoryReturnGesture(towardLatest: boolean) {
    historyReturnIntent.current = towardLatest;
    const container = transcriptRef.current;
    if (!towardLatest || !historyChromeCollapsed || !container) return;
    const distanceFromLatest = Math.max(0, container.scrollHeight - container.clientHeight - container.scrollTop);
    if (distanceFromLatest <= historyHeaderExpandDistance) restoreLatestChrome(container);
  }

  function onTranscriptWheel(event: ReactWheelEvent<HTMLDivElement>) {
    if (event.deltaY !== 0) {
      transcriptGestureDeadline.current = performance.now() + transcriptGestureGraceMs;
      applyHistoryReturnGesture(event.deltaY > 0);
    }
  }

  function onTranscriptTouchStart(event: ReactTouchEvent<HTMLDivElement>) {
    transcriptTouchY.current = event.touches[0]?.clientY ?? null;
  }

  function onTranscriptTouchMove(event: ReactTouchEvent<HTMLDivElement>) {
    const currentY = event.touches[0]?.clientY;
    const previousY = transcriptTouchY.current;
    if (currentY === undefined) return;
    transcriptTouchY.current = currentY;
    if (previousY === null || currentY === previousY) return;
    transcriptGestureDeadline.current = performance.now() + transcriptGestureGraceMs;
    applyHistoryReturnGesture(currentY < previousY);
  }

  function onTranscriptTouchEnd() {
    transcriptGestureDeadline.current = performance.now() + transcriptGestureGraceMs;
    transcriptTouchY.current = null;
  }

  function returnToLatest() {
    restoreLatestChrome();
    window.setTimeout(markCurrentThreadRead, 0);
  }

  function disconnect() {
    closePairingInvite();
    connectionResume.current = false;
    durableRestoreAttempt.current = "";
    selection.current = { deviceId: "", threadId: "", view: null };
    expectedSubscription.current = "";
    openFileGeneration.current++; setView(null); setThreadId(""); clearAllComposerDrafts(); setOpenFile(null); resetHistory();
    fileContentCache.current.clear(); fileContentCacheEpoch.current = "";
    void durableCache.clear();
    webSession.disconnect(); savedSessionAttempt.current = false; pairingAttempt.current = false; setSessionExpired(false); setToken(""); client.disconnect();
    if (webAuthMode === "passkey") {
      setPasskeyAuth(current => ({ ...current, loaded: true, error: "" }));
      setPasskeyBusy(true);
      void logoutPasskey()
        .catch(error => setPasskeyAuth(current => ({ ...current, error: error instanceof Error ? error.message : "退出失败，请重试。" })))
        .finally(() => setPasskeyBusy(false));
    }
  }

  async function authenticateWithPasskey() {
    if (passkeyBusy) return;
    setPasskeyBusy(true);
    setPasskeyAuth(current => ({ ...current, error: "" }));
    try {
      if (passkeyAuth.enrollmentRequired) await registerPasskey(setupToken);
      else await loginWithPasskey();
      setSetupToken("");
      setPasskeyAuth({ loaded: true, enrollmentRequired: false, error: "" });
      client.connect("cookie");
    } catch (error) {
      const message = error instanceof DOMException && error.name === "NotAllowedError"
        ? "Passkey 操作已取消；你可以重新尝试。"
        : error instanceof Error ? error.message : "Passkey 操作失败，请重试。";
      setPasskeyAuth(current => ({ ...current, error: message }));
    } finally {
      setPasskeyBusy(false);
    }
  }

  async function addPasskey() {
    if (passkeyBusy || webAuthMode !== "passkey") return;
    setPasskeyBusy(true);
    setNotice("");
    try {
      await registerPasskey("");
      setNotice("新的 Passkey 已添加。建议保留至少一个可用的备用凭据。");
    } catch (error) {
      const message = error instanceof DOMException && error.name === "NotAllowedError"
        ? "Passkey 操作已取消；你可以重新尝试。"
        : error instanceof Error ? error.message : "Passkey 操作失败，请重试。";
      setNotice(message);
    } finally {
      setPasskeyBusy(false);
    }
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
    const foundIDs = new Set(found.map(candidate => candidate.deviceId));
    for (const previous of devicesRef.current) {
      if (!foundIDs.has(previous.deviceId)) {
        historyCache.current.clearDevice(previous.deviceId);
        threadListCache.current.clearDevice(previous.deviceId);
      }
    }
    for (const candidate of found) {
      const previous = devicesRef.current.find(device => device.deviceId === candidate.deviceId);
      if (candidate.agentEpoch) observedAgentEpochs.current.set(candidate.deviceId, candidate.agentEpoch);
      if (previous?.agentEpoch && previous.agentEpoch !== candidate.agentEpoch) {
        clearWarmSubscriptions(candidate.deviceId);
        historyCache.current.clearDevice(candidate.deviceId);
        void durableCache.clearDevice(candidate.deviceId);
        threadListCache.current.clearDevice(candidate.deviceId);
        discardRestoredHistory(candidate.deviceId, selection.current.threadId);
        if (offlineDevice.current !== candidate.deviceId) invalidateOwnerView(candidate.deviceId, "Desktop Agent 已重连，正在重新同步当前会话。");
      }
    }
    const next = keepOfflineDevice(found, devicesRef.current, selection.current.deviceId);
    devicesRef.current = next;
    setDevices(next);
    setDeviceId(current => current || found[0]?.deviceId || "");
    return found;
  }

  function threadListIdentity(id: string, query: string): ThreadListCacheIdentity | null {
    const agentEpoch = devicesRef.current.find(candidate => candidate.deviceId === id)?.agentEpoch || observedAgentEpochs.current.get(id) || "";
    return id && agentEpoch ? { deviceId: id, agentEpoch, query } : null;
  }

  function observeUnread(id: string, observed: Thread[]) {
    const unread = unreadStore.observe(id, observed);
    const observedIDs = new Set(observed.map(thread => thread.threadId));
    setUnreadThreadIDs(current => {
      const updated = new Set(current);
      for (const threadID of observedIDs) updated.delete(threadID);
      for (const threadID of unread) updated.add(threadID);
      return updated;
    });
  }

  function markThreadRead(id: string, thread: Thread) {
    if (!unreadStore.markRead(id, thread.threadId, thread.updatedAt)) return;
    setUnreadThreadIDs(current => {
      if (!current.has(thread.threadId)) return current;
      const updated = new Set(current);
      updated.delete(thread.threadId);
      return updated;
    });
  }

  function markCurrentThreadRead() {
    const current = selection.current;
    if (document.visibilityState !== "visible" || !followLatestRef.current || !current.view?.subscriptionId || current.view.deviceId !== current.deviceId || current.view.threadId !== current.threadId) return;
    markThreadRead(current.deviceId, current.view.thread);
  }

  async function loadThreads(id: string, next = "", query = searchTerm, force = false, background = false) {
    const generation = ++threadListGeneration.current;
    if (!id) { setThreads([]); setListLoading(false); return; }
    const identity = threadListIdentity(id, query);
    if (!next && !force && identity) {
      const cached = threadListCache.current.get(identity);
      if (cached) {
        setThreads(cached.value.threads);
        observeUnread(id, cached.value.threads);
        setCursor(cached.value.cursor);
        setListError("");
        if (cached.fresh) { setListLoading(false); return; }
      }
    }
    if (!background) {
      setListLoading(true);
      setListError("");
    }
    threadListRequestsInFlight.current++;
    const response = await client.request("thread.list", id, { limit: 50, ...(next ? { cursor: next } : {}), ...(query ? { searchTerm: query } : {}) })
      .finally(() => { threadListRequestsInFlight.current--; });
    const currentIdentity = threadListIdentity(id, query);
    if (generation !== threadListGeneration.current || selection.current.deviceId !== id || searchInputRef.current.trim() !== query || identity?.agentEpoch !== currentIdentity?.agentEpoch) return;
    if (!background) setListLoading(false);
    if (response.outcome !== "accepted") {
      if (background) return;
      const code = response.error?.code;
      setListError(query && code === "PROTOCOL_UNSUPPORTED" ? "当前 Codex 版本不支持会话内容搜索" : code === "TIMEOUT" || code === "OUTCOME_UNKNOWN" ? "加载会话超时，请重试。" : errorText(response));
      return;
    }
    const page = (response.data?.threads as Thread[] | undefined) || [];
    observeUnread(id, page);
    const nextCursor = String(response.data?.nextCursor || "");
    setThreads(current => {
      const merged = next ? mergeThreadPages(current, page) : page;
      if (identity) threadListCache.current.set(identity, { threads: merged, cursor: nextCursor });
      return merged;
    });
    setCursor(nextCursor);
  }

  async function refreshUnreadThreads(allowClosedList = false) {
    if (backgroundThreadListRefresh.current || threadListRequestsInFlight.current > 0 || status !== "ready" || !deviceId || !device?.agentOnline || !device.codexReady || searchInputRef.current.trim() || document.visibilityState !== "visible" || (!allowClosedList && !showList)) return;
    backgroundThreadListRefresh.current = true;
    try { await loadThreads(deviceId, "", "", true, true); }
    finally { backgroundThreadListRefresh.current = false; }
  }

  function openNewThreadDialog() {
    if (!threadCreateEnabled || !deviceId) return;
	if (localThreadDraft && (draft.trim() || screenshots.length) && !window.confirm("当前新会话草稿尚未发送。要放弃它并重新选择项目吗？")) return;
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

  function createThread() {
    const cwd = newThreadCwd.trim();
    if (!threadCreateEnabled || !deviceId || !cwd || creatingThread) return;
    const oldSubscription = expectedSubscription.current;
    const oldDevice = selection.current.deviceId;
    if (oldSubscription && oldDevice) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    dropWarmSubscription(oldDevice, selection.current.threadId);
    expectedSubscription.current = "";
    pendingSelect.current++;
    selection.current = { deviceId, threadId: "", view: null };
    setThreadId("");
    setView(null);
    setSyncingView(false);
    setReadOnlyHistory(false);
    resetHistory();
    const draftId = `local:${newRequestID()}`;
    setLocalThreadDraft({ deviceId, agentEpoch: agentEpochFor(deviceId), cwd, draftId });
    activateComposer({ deviceId, threadId: draftId });
    setNotice("");
    setNewThreadOpen(false);
    setShowList(false);
    window.setTimeout(() => composerInputRef.current?.focus(), 0);
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

  async function showReadOnlyHistory(id: string, targetDevice: string, epoch: number, prefetched?: Thread | null): Promise<boolean> {
    let thread: Thread | null | undefined = prefetched;
    if (thread === undefined) {
      const pending = fastReadCache.current;
      if (pending && pending.deviceId === targetDevice && pending.threadId === id && !pending.settled) {
        // A progressive fast path for this thread is still in flight; reuse it
        // instead of issuing a duplicate thread.read.
        thread = await pending.result;
      } else {
        const stored = await client.request("thread.read", targetDevice, { threadId: id });
        if (epoch !== pendingSelect.current || selection.current.threadId !== id || selection.current.deviceId !== targetDevice) return false;
        const fetched = stored.data?.thread as Thread | undefined;
        if (stored.outcome !== "accepted" || fetched?.threadId !== id) {
          setNotice(stored.outcome === "accepted" ? "历史页身份无法确认。" : errorText(stored));
          return false;
        }
        thread = fetched;
      }
    }
    if (epoch !== pendingSelect.current || selection.current.threadId !== id || selection.current.deviceId !== targetDevice) return false;
    if (!thread || thread.threadId !== id) {
      setNotice("历史页身份无法确认。");
      return false;
    }
    const readOnlyThread: Thread = { ...thread, runtime: "unknown", pendingInteractions: [], permissions: { sandbox: "unknown", approval: "unknown" } };
    const readOnlyView: ThreadView = { deviceId: targetDevice, threadId: id, subscriptionId: "", streamId: "", seq: 0, thread: readOnlyThread };
    selection.current.view = readOnlyView;
    setView(readOnlyView);
    setSyncingView(false);
    setReadOnlyHistory(true);
    setNotice("");
    return true;
  }

  async function selectThread(id: string, targetDevice = deviceId, cachePrevious = true, retryCreatedSubscribe = false, initialThread?: Thread) {
    if (!targetDevice) return;
    if (cachePrevious) cacheCurrentHistory();
    const sameThread = selection.current.deviceId === targetDevice && selection.current.threadId === id;
    if (!sameThread) activateComposer({ deviceId: targetDevice, threadId: id });
    if (sameThread) rememberReadingPosition();
    else { resumeReadingAnchor.current = null; lastTranscriptScrollTop.current = 0; restoreLatestChrome(); }
    pendingReadingAnchor.current = null;
    blockedSelection.current = "";
    const epoch = ++pendingSelect.current;
    const old = selection.current.view;
    const oldDevice = selection.current.deviceId;
    const oldSubscription = expectedSubscription.current;
    if (old?.subscriptionId) retainWarmSubscription(old);
    const targetKey = warmSubscriptionKey(targetDevice, id);
    const oldKey = old?.subscriptionId ? warmSubscriptionKey(old.deviceId, old.threadId) : "";
    pruneWarmSubscriptions(new Set([targetKey, oldKey].filter(Boolean)));
    const warm = warmSubscription(targetDevice, id);
    expectedSubscription.current = "";
    const restoredView = restoreCachedConversation(targetDevice, id);
    const cachedView = warm || restoredView || (initialThread ? { deviceId: targetDevice, threadId: id, subscriptionId: "", streamId: "", seq: 0, thread: initialThread } : null);
    selection.current = { deviceId: targetDevice, threadId: id, view: cachedView };
    expectedSubscription.current = warm?.subscriptionId || "";
    setThreadId(id); setView(cachedView); setSyncingView(!warm); setReadOnlyHistory(false); setNotice(""); setShowList(false); setQueueWorking(""); setQueueMenuId(""); cancelQueueDrag();
    if (!restoredView) resetHistory();
    if (warm) { persistView(warm); return; }
    if (oldSubscription && !old?.subscriptionId) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    // Progressive open (spec 0086): without any cached view, race the read-only
    // fast path against the owner subscription so history renders within
    // seconds instead of after a full owner cold load.
    let progressiveInstalled = false;
    let subscribeSettled = false;
    if (!cachedView) {
      const entry = {
        deviceId: targetDevice,
        threadId: id,
        epoch,
        settled: false,
        thread: null as Thread | null,
        result: Promise.resolve(null as Thread | null),
      };
      entry.result = client.request("thread.read", targetDevice, { threadId: id }).then(stored => {
        const thread = stored.data?.thread as Thread | undefined;
        entry.thread = stored.outcome === "accepted" && thread?.threadId === id ? thread : null;
        entry.settled = true;
        return entry.thread;
      }).catch(() => { entry.settled = true; return null; });
      fastReadCache.current = entry;
      void entry.result.then(thread => {
        if (!thread || subscribeSettled) return;
        if (epoch !== pendingSelect.current || selection.current.threadId !== id || selection.current.deviceId !== targetDevice) return;
        if (expectedSubscription.current || selection.current.view?.subscriptionId) return;
        const readOnlyThread: Thread = { ...thread, runtime: "unknown", pendingInteractions: [], permissions: { sandbox: "unknown", approval: "unknown" } };
        const pendingView: ThreadView = { deviceId: targetDevice, threadId: id, subscriptionId: "", streamId: "", seq: 0, thread: readOnlyThread };
        progressiveInstalled = true;
        selection.current.view = pendingView;
        setView(pendingView);
        setSyncingView(true);
      });
    } else {
      fastReadCache.current = null;
    }
    let response = await client.request("thread.subscribe", targetDevice, { threadId: id, updateMode: "item-delta-v1" });
    subscribeSettled = true;
    if (epoch !== pendingSelect.current) {
      const staleID = response.data?.subscriptionId;
      if (response.outcome === "accepted" && typeof staleID === "string") void client.request("thread.unsubscribe", targetDevice, { subscriptionId: staleID });
      return;
    }
    if (retryCreatedSubscribe && response.outcome !== "accepted" && (response.error?.code === "RESYNC_REQUIRED" || response.error?.code === "TIMEOUT")) {
      await new Promise(resolve => window.setTimeout(resolve, 500));
      if (epoch !== pendingSelect.current || selection.current.deviceId !== targetDevice || selection.current.threadId !== id || status !== "ready") return;
      response = await client.request("thread.subscribe", targetDevice, { threadId: id, updateMode: "item-delta-v1" });
      if (epoch !== pendingSelect.current) {
        const staleID = response.data?.subscriptionId;
        if (response.outcome === "accepted" && typeof staleID === "string") void client.request("thread.unsubscribe", targetDevice, { subscriptionId: staleID });
        return;
      }
    }
    if (response.outcome !== "accepted") {
      const entry = fastReadCache.current;
      const fast = entry && entry.epoch === epoch && entry.deviceId === targetDevice && entry.threadId === id ? entry : null;
      const oversized = response.error?.code === "HISTORY_TOO_LARGE" || response.error?.code === "OVERLOADED";
      // Oversized owner frames need the persisted history before degrading, so
      // wait for an in-flight fast path. That wait is bounded above by the
      // Relay request timeout backing the fast read, so it cannot hang the
      // recovery path indefinitely. Other failures must not block recovery
      // loops on a read that may never resolve.
      const fastThread = fast ? (fast.settled ? fast.thread : oversized ? await fast.result : null) : null;
      if (epoch !== pendingSelect.current) return;
      if (oversized) {
        setReadOnlyHistory(true);
        if (progressiveInstalled) { setSyncingView(false); return; }
        if (fast && !fastThread) {
          // The fast path already failed; do not issue a duplicate read.
          blockedSelection.current = `${targetDevice}\u0000${id}`;
          setNotice("原 owner 状态无法确认，历史分页也未能加载。可稍后手动重新选择此会话。");
          return;
        }
        if (await showReadOnlyHistory(id, targetDevice, epoch, fast ? fastThread : undefined)) return;
        blockedSelection.current = `${targetDevice}\u0000${id}`;
        setNotice("原 owner 状态无法确认，历史分页也未能加载。可稍后手动重新选择此会话。");
        return;
      }
      if (progressiveInstalled || fastThread) {
        // Owner sync failed but persisted history is already readable: keep it
        // as an explicit read-only degradation instead of a blank page.
        if (response.error?.code === "NATIVE_STATE_UNCERTAIN") blockedSelection.current = `${targetDevice}\u0000${id}`;
        if (!progressiveInstalled && fastThread && !(await showReadOnlyHistory(id, targetDevice, epoch, fastThread))) {
          setNotice(errorText(response));
          return;
        }
        setReadOnlyHistory(true);
        setSyncingView(false);
        setNotice(`${errorText(response)} 历史保持只读；重新选择会话可重试连接。`);
        return;
      }
      if (response.error?.code === "NATIVE_STATE_UNCERTAIN") blockedSelection.current = `${targetDevice}\u0000${id}`;
      setNotice(errorText(response)); return;
    }
    const subID = response.data?.subscriptionId;
    if (typeof subID !== "string" || !subID) { setNotice("订阅回执缺少 ID，无法确认会话状态。"); return; }
    expectedSubscription.current = subID;
  }

  async function resumeSelected() {
    recoveryGeneration.current++;
    if (recoveryRunning.current) return;
    recoveryRunning.current = true;
    try {
      for (;;) {
        const generation = recoveryGeneration.current;
        const current = selection.current;
        const online = devicesRef.current.filter(candidate => candidate.agentOnline);
        const target = recoveryTarget(current.deviceId, current.threadId, online, blockedSelection.current);
        if (target && (!current.view || !current.view.subscriptionId)) await selectThread(target.threadId, target.deviceId);
        if (generation === recoveryGeneration.current) return;
      }
    } finally {
      recoveryRunning.current = false;
    }
  }

  async function resync() {
    const current = selection.current;
    if (!current.threadId) return;
    cacheCurrentHistory();
    dropWarmSubscription(current.deviceId, current.threadId, true);
    expectedSubscription.current = "";
    if (current.view) {
      const stale = { ...current.view, subscriptionId: "", streamId: "", seq: 0 };
      selection.current.view = stale;
      setView(stale);
    }
    setSyncingView(true);
    setNotice("");
    await selectThread(current.threadId, current.deviceId, false);
  }

  useEffect(() => {
    client.onStatus = next => {
      setStatus(next);
      if (next === "invalid") {
        durableRestoreAttempt.current = "";
        if (webAuthMode === "passkey") {
          setPasskeyAuth(current => ({ ...current, loaded: true, error: "登录会话已失效，请重新使用 Passkey。" }));
        } else {
          webSession.rejected();
          if (pairingAttempt.current) {
            setPhonePairing(current => current ? { credential: "", phase: "error" } : current);
          } else if (savedSessionAttempt.current) setSessionExpired(true);
        }
        pairingAttempt.current = false;
        savedSessionAttempt.current = false;
        connectionResume.current = false;
        rememberReadingPosition(); clearAgentOfflineTimer(); offlineDevice.current = ""; observedAgentEpochs.current.clear(); warmSubscriptions.current.clear(); setRecoveringDeviceId(""); historyCache.current.clear(); threadListCache.current.clear(); pendingSelect.current++; deviceListGeneration.current++; threadListGeneration.current++; threadCreateGeneration.current++; expectedSubscription.current = ""; selection.current.view = null; setView(null); setLocalThreadDraft(null); setSyncingView(false); setReadOnlyHistory(false); setNewThreadOpen(false); setCreatingThread(false); resetHistory(); setPairingInvite(current => current?.status === "waiting" ? null : current);
        return;
      }
      if (next !== "ready") {
        rememberReadingPosition(); cacheCurrentHistory(); clearAgentOfflineTimer(); offlineDevice.current = ""; setRecoveringDeviceId(""); pendingSelect.current++; deviceListGeneration.current++; threadListGeneration.current++; threadCreateGeneration.current++; expectedSubscription.current = ""; warmSubscriptions.current.clear();
        if (selection.current.view) {
          connectionResume.current = true;
          const stale = { ...selection.current.view, subscriptionId: "", streamId: "", seq: 0 };
          selection.current.view = stale;
          setView(stale);
          setSyncingView(true);
        }
        setPairingInvite(current => current?.status === "waiting" ? null : current);
      }
    };
    client.onReady = (_epoch, sessionToken) => {
      warmSubscriptions.current.clear();
      if (webAuthMode === "pin" && sessionToken) webSession.accepted(sessionToken);
      if (pairingAttempt.current) setPhonePairing(null);
      pairingAttempt.current = false;
      savedSessionAttempt.current = webAuthMode === "pin" && Boolean(sessionToken);
      setSessionExpired(false);
      void refreshDevices().then(() => void resumeSelected());
    };
    client.onEvent = (event: ArielProtocolV1Envelope) => {
      if (event.type !== "event") return;
      if (event.event === "auth.pair.consumed") {
        setPairingInvite(current => current?.status === "waiting" ? { ...current, status: "consumed", credential: "", qr: "" } : current);
        return;
      }
      if (event.event === "device.status") {
        const eventEpoch = event.agentEpoch;
        const observedEpoch = observedAgentEpochs.current.get(event.deviceId);
        if (event.agentOnline) {
          if (eventEpoch) observedAgentEpochs.current.set(event.deviceId, eventEpoch);
        } else {
          if (eventEpoch && observedEpoch && eventEpoch !== observedEpoch) return;
        }
        updateDevicePresence(event);
        if (!event.agentOnline) {
          clearWarmSubscriptions(event.deviceId);
          historyCache.current.clearDevice(event.deviceId);
          if (event.deviceId === selection.current.deviceId) {
            rememberReadingPosition();
            beginAgentOfflineGrace(event.deviceId);
          }
        }
        void refreshDevices().then(online => {
          const recovered = online.some(candidate => candidate.deviceId === event.deviceId && candidate.agentOnline && candidate.codexReady);
          if (event.agentOnline && recovered && offlineDevice.current === event.deviceId && recoveryTarget(selection.current.deviceId, selection.current.threadId, online, blockedSelection.current)) {
            clearAgentOfflineTimer();
            expectedSubscription.current = "";
            selection.current.view = null;
            setSyncingView(true);
          }
          void resumeSelected();
        });
        return;
      }
      if (event.event === "interaction.resolved") {
        if (event.threadId === selection.current.threadId) setNotice("交互已完成，会话状态已同步。");
        return;
      }
      const current = selection.current;
      const currentEvent = belongsToSubscription(event, current.deviceId, current.threadId, expectedSubscription.current);
      const warm = warmSubscription(event.deviceId, event.threadId);
      const warmEvent = Boolean(warm && belongsToSubscription(event, warm.deviceId, warm.threadId, warm.subscriptionId));
      if (!currentEvent && !warmEvent) return;
      if (event.event === "thread.error") {
        dropWarmSubscription(event.deviceId, event.threadId);
        if (!currentEvent) return;
        if (!current.view || !current.view.subscriptionId || (current.view.subscriptionId === event.subscriptionId && current.view.streamId === event.streamId)) {
          if (event.code === "HISTORY_TOO_LARGE") {
            const identity = historyCacheIdentity(current.deviceId, current.threadId);
            if (identity) historyCache.current.delete(identity);
            expectedSubscription.current = "";
            setReadOnlyHistory(true);
            setSyncingView(false);
            void showReadOnlyHistory(current.threadId, current.deviceId, pendingSelect.current);
            return;
          }
          if (event.code === "RESYNC_REQUIRED") {
            expectedSubscription.current = "";
            setSyncingView(true);
            void resync();
            return;
          }
          const identity = historyCacheIdentity(current.deviceId, current.threadId);
          if (identity) historyCache.current.delete(identity);
          if (event.code === "NATIVE_STATE_UNCERTAIN") blockedSelection.current = `${current.deviceId}\u0000${current.threadId}`;
          expectedSubscription.current = "";
          selection.current.view = null;
          setView(null);
          setSyncingView(false);
          setNotice(resultText[event.code]);
        }
        return;
      }
      if (event.event === "thread.snapshot") {
        if (!currentEvent) return;
        // Preserve the reading position across the snapshot handoff — including
        // a progressive read-only view (spec 0086) that has no subscription yet.
        // A resync may already hold an anchor captured while the position was
        // still valid; never overwrite a pending anchor from the stale view.
        if (current.view && current.view.deviceId === event.deviceId && current.view.threadId === event.threadId && !resumeReadingAnchor.current) rememberReadingPosition();
        const next = applyThreadEvent(null, event);
        if (!next) return;
        if (!restoreCachedHistory(event.deviceId, event.threadId, event.thread.turns)) resetHistory();
        observeUnread(event.deviceId, [event.thread]);
        retainWarmSubscription(next);
        connectionResume.current = false;
        selection.current.view = next; setView(next); setSyncingView(false); setReadOnlyHistory(false); setNotice(""); finishAgentRecovery(event.deviceId); reconcileSubmission(event.thread, event.deviceId, event.threadId);
        persistView(next);
      } else if (event.event === "thread.update") {
        const next = applyThreadEvent(currentEvent ? current.view : warm, event);
        if (!next) {
          dropWarmSubscription(event.deviceId, event.threadId, true);
          if (currentEvent) void resync();
          return;
        }
        observeUnread(event.deviceId, [next.thread]);
        retainWarmSubscription(next);
        if (!currentEvent) cacheWarmThread(next);
        else { selection.current.view = next; setView(next); persistView(next); }
        setThreads(list => list.map(t => t.threadId === next.threadId ? { ...next.thread, searchSnippet: t.searchSnippet } : t));
        if (currentEvent) reconcileSubmission(next.thread, event.deviceId, event.threadId);
      }
    };
    if (webAuthMode === "pin" && !phonePairing) {
      const saved = webSession.saved();
      if (saved) { savedSessionAttempt.current = true; client.connect(saved); }
    }
    return () => { clearAgentOfflineTimer(); client.disconnect(); };
  }, [client, webAuthMode]);

  useEffect(() => {
    if (webAuthMode !== "passkey") return;
    let active = true;
    void loadAuthStatus().then(auth => {
      if (!active) return;
      if (auth.mode !== "passkey") throw new Error("Relay 返回了不一致的认证模式。");
      setPasskeyAuth({ loaded: true, enrollmentRequired: auth.enrollmentRequired, error: "" });
      if (auth.authenticated) client.connect("cookie");
    }).catch(error => {
      if (active) setPasskeyAuth({ loaded: true, enrollmentRequired: false, error: error instanceof Error ? error.message : "无法读取认证状态。" });
    });
    return () => { active = false; };
  }, [client, webAuthMode]);

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

  useLayoutEffect(() => {
    if (activeComposerIdentity.current) composerDrafts.current.set(activeComposerIdentity.current, captureComposerSnapshot());
    activeComposerIdentity.current = null;
    showComposerSnapshot(emptyComposerDraft());
    pendingSelect.current++;
    threadListGeneration.current++;
    const oldDevice = selection.current.deviceId;
    if (offlineDevice.current && offlineDevice.current !== deviceId) finishAgentRecovery(offlineDevice.current);
    if (oldDevice && oldDevice !== deviceId) { resumeReadingAnchor.current = null; followLatestRef.current = true; setShowReturnToLatest(false); }
    const oldSubscription = expectedSubscription.current;
    const oldWarm = oldDevice ? warmSubscription(oldDevice, selection.current.threadId) : null;
    if (oldDevice) clearWarmSubscriptions(oldDevice, true);
    if (oldDevice && oldSubscription && !oldWarm) void client.request("thread.unsubscribe", oldDevice, { subscriptionId: oldSubscription });
    expectedSubscription.current = "";
    selection.current = { deviceId, threadId: "", view: null };
    const cached = threadListIdentity(deviceId, searchTerm);
    const cachedList = cached ? threadListCache.current.get(cached) : null;
    const cachedThreads = cachedList?.value.threads || [];
    setUnreadThreadIDs(cachedThreads.length && deviceId ? unreadStore.observe(deviceId, cachedThreads) : new Set());
    threadCreateGeneration.current++; setNewThreadOpen(false); setCreatingThread(false); setLocalThreadDraft(null); setExpandedProjectKeys(new Set()); setThreadId(""); setView(null); setSyncingView(false); setReadOnlyHistory(false); setThreads(cachedThreads); setCursor(cachedList?.value.cursor || ""); setListError(""); setQueueWorking(""); setQueueMenuId(""); cancelQueueDrag(); resetHistory();
  }, [deviceId]);

  useEffect(() => {
    if (sessionExpired) clearAllComposerDrafts();
  }, [sessionExpired]);

  useEffect(() => {
    const epoch = device?.agentEpoch || "";
    const attempt = status === "ready" && deviceId && epoch ? `${deviceId}\u0000${epoch}` : "";
    if (!attempt || durableRestoreAttempt.current === attempt || selection.current.threadId) return;
    durableRestoreAttempt.current = attempt;
    let active = true;
    void durableCache.lastSelection().then(async identity => {
      if (!active || !identity || identity.deviceId !== deviceId || identity.agentEpoch !== epoch || selection.current.threadId) return;
      const cached = await durableCache.get(identity);
      if (!active || !cached?.thread || cached.thread.threadId !== identity.threadId || selection.current.threadId) return;
      if (!historyCache.current.set(identity, cached)) return;
      connectionResume.current = true;
      await selectThread(identity.threadId, identity.deviceId, false);
    });
    return () => { active = false; };
  }, [status, deviceId, device?.agentEpoch, durableCache]);

  useEffect(() => {
    if (!searchInputChanged.current) return;
    const timer = window.setTimeout(() => {
      setSearchTerm(searchInput.trim());
      setSearchRevision(current => current + 1);
    }, 250);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  useEffect(() => {
    const force = searchInputChanged.current;
    searchInputChanged.current = false;
    if (status === "ready" && deviceId) void loadThreads(deviceId, "", searchTerm, force);
  }, [deviceId, status, searchRevision]);

  useEffect(() => {
    let hiddenAt = 0;
    const onVisibility = () => {
      if (document.visibilityState === "hidden") hiddenAt = Date.now();
      else {
        markCurrentThreadRead();
        if (hiddenAt && Date.now() - hiddenAt >= 5000) {
          hiddenAt = 0;
          void (async () => {
            if (webAuthMode === "passkey") {
              try {
                const auth = await loadAuthStatus();
                if (auth.mode === "passkey") setPasskeyAuth({ loaded: true, enrollmentRequired: auth.enrollmentRequired, error: "" });
              } catch { /* WebSocket state remains authoritative. */ }
            }
            client.wake();
            void refreshUnreadThreads();
          })();
        }
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, [client, webAuthMode]);

  useEffect(() => {
    if (webAuthMode !== "passkey" || status !== "ready") return;
    const timer = window.setInterval(() => { void loadAuthStatus().catch(() => undefined); }, 12 * 60 * 60_000);
    return () => window.clearInterval(timer);
  }, [webAuthMode, status]);

  useEffect(() => {
    if (status !== "ready" || !deviceId || !device?.agentOnline || !device.codexReady || searchTerm || !showList || document.visibilityState !== "visible") return;
    const timer = window.setInterval(() => void refreshUnreadThreads(), 15_000);
    return () => window.clearInterval(timer);
  }, [status, deviceId, device?.agentOnline, device?.codexReady, searchTerm, showList]);

  useEffect(() => {
    if (!view?.subscriptionId || syncingView || readOnlyHistory) return;
    markCurrentThreadRead();
  }, [view?.threadId, view?.thread.updatedAt, view?.seq, syncingView, readOnlyHistory]);

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
    setThreadMenu(null);
    setRenameThreadDialog(null);
    setRenameThreadError("");
    setArchiveUndoToast(null);
  }, [deviceId, device?.agentEpoch]);

  const renameThreadDialogID = renameThreadDialog?.threadId || "";
  const renameThreadDialogOpen = renameThreadDialog !== null;

  useEffect(() => {
    if (!renameThreadDialogID) return;
    renameThreadInput.current?.focus();
    renameThreadInput.current?.select();
  }, [renameThreadDialogID]);

  useEffect(() => {
    if (!renameThreadDialogOpen) return;
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape" && !threadMetadataWorking) setRenameThreadDialog(null); };
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [renameThreadDialogOpen, threadMetadataWorking]);

  useEffect(() => {
    if (!threadMenu) return;
    threadMenuFirstItem.current?.focus();
    const close = (event: PointerEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest(".thread-context-menu")) setThreadMenu(null);
    };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") setThreadMenu(null); };
    document.addEventListener("pointerdown", close);
    window.addEventListener("keydown", escape);
    return () => { document.removeEventListener("pointerdown", close); window.removeEventListener("keydown", escape); };
  }, [threadMenu]);

  useEffect(() => {
    if (!archiveUndoToast) return;
    const timer = window.setTimeout(() => setArchiveUndoToast(null), 8_000);
    return () => window.clearTimeout(timer);
  }, [archiveUndoToast]);

  useEffect(() => {
    if (!syncingView && view && editingQueueId && !queuedMessages.some(item => item.queueId === editingQueueId && item.editable)) {
      setEditingQueueId("");
      setNotice("原排队消息已变化，请核对后重新发送。");
    }
  }, [editingQueueId, queuedMessages, syncingView, view]);

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

  useEffect(() => () => cancelLatestChromeRestore(), []);

  useLayoutEffect(() => {
    const input = composerInputRef.current;
    if (!input) return;
    const resize = () => {
      const previousScrollTop = input.scrollTop;
      const caretAtEnd = input.selectionEnd === input.value.length;
      input.style.height = "";
      if (!input.value) {
        input.style.height = `${composerMinHeight}px`;
        input.scrollTop = 0;
        return;
      }
      input.style.height = "auto";
      const contentHeight = input.scrollHeight;
      input.style.height = `${Math.max(composerMinHeight, Math.min(contentHeight, composerMaxHeight))}px`;
      input.scrollTop = contentHeight > composerMaxHeight ? (caretAtEnd ? contentHeight : previousScrollTop) : 0;
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
    const onBlur = () => {
      setVisualViewportHeight(null);
    };
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
    // While a previously painted view resyncs, leave the scroll position alone
    // until the fresh owner arrives. A progressive first-open view (spec 0086)
    // has not been painted yet, so it still gets the initial latest-position.
    if (syncingView && !view.subscriptionId && lastPaint.current.key === key) return;
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
      else { restoreLatestChrome(container); setNotice("原阅读位置已不在当前历史窗口，已回到最新消息。"); }
      resumeReadingAnchor.current = null;
      lastPaint.current = { key, seq: view.seq, marker };
      return;
    }
    if (lastPaint.current.key !== key) {
      restoreLatestChrome(container);
    } else if (lastPaint.current.seq !== view.seq) {
      if (followLatestRef.current) scrollToLatest(container);
      else if (hasNewVisibleContent(lastPaint.current.marker, marker)) setShowReturnToLatest(true);
    } else if (followLatestRef.current) scrollToLatest(container);
    lastPaint.current = { key, seq: view.seq, marker };
  }, [view?.seq, view?.threadId, history, itemOverrides, syncingView]);

  async function loadOlder(additionalTurns = 1, requestLimit = historyTurnPageLimit) {
    if (!view || !deviceId || historyLoadingRef.current || historyRef.current.exhausted) return;
    const epoch = pendingSelect.current;
    const targetThread = view.threadId;
    const targetCount = historyRef.current.older.length + additionalTurns;
    historyLoadingRef.current = true;
    setHistoryLoading(true);
    try {
      for (let pageNumber = 0; pageNumber < 20; pageNumber++) {
        const current = historyRef.current;
        const response = await client.request("thread.history", deviceId, { threadId: targetThread, limit: requestLimit, ...(current.cursor ? { cursor: current.cursor } : {}) });
        if (epoch !== pendingSelect.current || selection.current.threadId !== targetThread) return;
        if (response.outcome !== "accepted") {
          if (response.error?.code === "INVALID_ARGUMENT" && discardRestoredHistory(deviceId, targetThread)) continue;
          setNotice(errorText(response)); return;
        }
        const turns = response.data?.turns as Turn[] | undefined;
        const nextCursor = response.data?.nextCursor;
        if (!Array.isArray(turns) || typeof nextCursor !== "string") {
          if (discardRestoredHistory(deviceId, targetThread)) continue;
          setNotice("历史页格式无法识别，已保留当前会话。"); return;
        }
        const next = appendOlderPage(current, selection.current.view?.thread.turns || view.thread.turns, { turns, nextCursor });
        if (next.cursor === current.cursor && !next.exhausted && next.older.length === current.older.length) {
          if (discardRestoredHistory(deviceId, targetThread)) continue;
          setNotice("历史页没有继续前进，请稍后重试。"); return;
        }
        pendingReadingAnchor.current = captureReadingAnchor(transcriptRef.current, readingKey(deviceId, targetThread));
        historyRef.current = next;
        setHistory(next);
        if (selection.current.view) persistView(selection.current.view);
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
    if (missing > 0) void loadOlder(missing, Math.min(missing, recentTurnLimit));
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
      if (response.outcome !== "accepted") {
        if (response.error?.code === "INVALID_ARGUMENT" && discardRestoredHistory(deviceId, targetThread)) void loadOlder();
        else setNotice(errorText(response));
        return;
      }
      const items = response.data?.items as Item[] | undefined;
      const nextItemCursor = response.data?.nextItemCursor;
      const itemsComplete = response.data?.itemsComplete;
      if (response.data?.turnId !== turn.turnId || !Array.isArray(items) || typeof nextItemCursor !== "string" || typeof itemsComplete !== "boolean") { setNotice("消息页格式无法识别，已保留当前内容。"); return; }
      pendingReadingAnchor.current = captureReadingAnchor(transcriptRef.current, readingKey(deviceId, targetThread));
      const next = { ...itemOverridesRef.current, [turn.turnId]: prependOlderItems(itemOverridesRef.current[turn.turnId] || turn, items, nextItemCursor, itemsComplete) };
      replaceItemOverrides(next);
      if (selection.current.view) persistView(selection.current.view);
    } finally {
      itemLoadingRef.current = "";
      setItemLoading("");
    }
  }

  function applyQueueResponse(response: Response, targetDevice: string, targetThread: string): boolean {
    const items = responseQueue(response);
    if (!items) return false;
    if (selection.current.deviceId !== targetDevice || selection.current.threadId !== targetThread || !selection.current.view) return true;
    const next = { ...selection.current.view, thread: { ...selection.current.view.thread, queuedMessages: items } };
    selection.current.view = next;
    setView(next);
    return true;
  }

  async function send() {
	if (localThreadDraft) {
		if (!deviceId || working || readOnlyHistory || !composerReady || (!draft.trim() && screenshots.length === 0)) return;
		const local = localThreadDraft;
		const sourceSnapshot = captureComposerSnapshot();
		const revision = sourceSnapshot.revision;
		const text = draft;
		const images = screenshots;
		const clientMessageId = newRequestID();
		setWorking(true);
		setNotice("");
		const response = await client.request("thread.bootstrap", local.deviceId, { cwd: local.cwd, clientMessageId, text, ...(images.length ? { images: images.map(image => image.dataUri) } : {}) });
		setWorking(false);
		if (selection.current.deviceId !== local.deviceId || agentEpochFor(local.deviceId) !== local.agentEpoch) return;
		const candidate = response.data?.thread as Partial<Thread> | undefined;
		const receiptThreadID = response.data?.threadId;
		const receiptTurnID = response.data?.turnId;
		const accepted = response.outcome === "accepted" && candidate && typeof candidate.threadId === "string" && candidate.threadId === receiptThreadID
			&& typeof candidate.cwd === "string" && candidate.cwd === local.cwd && typeof candidate.title === "string" && typeof candidate.updatedAt === "string"
			&& Array.isArray(candidate.turns) && Array.isArray(candidate.pendingInteractions) && typeof receiptTurnID === "string" && receiptTurnID
			&& response.data?.clientMessageId === clientMessageId;
		if (!accepted) {
			setNotice(response.outcome === "accepted" ? "创建结果无法确认。草稿已保留，请先刷新会话列表核对，不要直接重发。" : errorText(response));
			return;
		}
		const created = candidate as Thread;
		const currentSnapshot = composerSnapshot.current;
		const temporaryIdentity = { deviceId: local.deviceId, threadId: local.draftId };
		const createdIdentity = { deviceId: local.deviceId, threadId: created.threadId };
		composerDrafts.current.set(createdIdentity, currentSnapshot.revision === revision ? emptyComposerDraft(currentSnapshot.revision + 1) : currentSnapshot);
		composerDrafts.current.delete(temporaryIdentity);
		activeComposerIdentity.current = null;
		setLocalThreadDraft(null);
		setThreads(current => [created, ...current.filter(thread => thread.threadId !== created.threadId)]);
		await selectThread(created.threadId, local.deviceId, false, false, created);
		return;
	}
    if (!view || !deviceId || readOnlyHistory || !canSend(view.thread, ownerReady, working || !!queueWorking, draft, screenshots.length + fileQuotes.length, queueEnabled)) return;
    const text = draft;
    const images = screenshots;
    const targetDevice = deviceId;
    const targetThread = view.threadId;
    const queueID = editingQueueId;
    const clientMessageId = queueID ? "" : newRequestID();
    const sourceSnapshot = captureComposerSnapshot();
    composerDrafts.current.set({ deviceId: targetDevice, threadId: targetThread }, sourceSnapshot);
    const submission: PendingSubmission = { deviceId: targetDevice, agentEpoch: agentEpochFor(targetDevice), threadId: targetThread, clientMessageId, revision: sourceSnapshot.revision, queueId: queueID };
    if (clientMessageId) pendingSubmissions.current.set(clientMessageId, submission);
    setWorking(true); setNotice("");
    const content = { text, ...(images.length ? { images: images.map(image => image.dataUri) } : {}), ...(!queueID && fileQuotes.length ? { fileContexts:fileQuotes } : {}) };
    const response = queueID
      ? await client.request("queue.update", targetDevice, { threadId: targetThread, queueId: queueID, ...content })
      : view.thread.runtime === "inProgress" && queueEnabled
        ? await client.request("queue.add", targetDevice, { threadId: targetThread, clientMessageId, ...content })
        : await client.request("turn.start", targetDevice, { threadId: targetThread, clientMessageId, ...content });
    setWorking(false);
    const queueMutation = !!queueID || (view.thread.runtime === "inProgress" && queueEnabled);
    const accepted = response.outcome === "accepted" && (!queueMutation || applyQueueResponse(response, targetDevice, targetThread));
    if (accepted) {
      if (clientMessageId) pendingSubmissions.current.delete(clientMessageId);
      clearSubmittedComposer(submission);
      return;
    }
    if (response.outcome !== "unknown" && clientMessageId) pendingSubmissions.current.delete(clientMessageId);
    if (sameSubmissionTarget(submission)) setNotice(response.outcome === "accepted" ? "队列回执格式无法确认，草稿已保留。" : errorText(response));
  }

  function editQueuedMessage(item: QueuedMessage) {
    if (!item.editable || !ownerReady || working || queueWorking) return;
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
    if (!view || !deviceId || !ownerReady || working || queueWorking || readOnlyHistory) return;
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
    if (!applyQueueResponse(response, targetDevice, targetThread)) { setNotice("队列回执格式无法确认，请等待状态同步后再操作。"); return; }
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
    if (response.outcome !== "accepted" || typeof response.data?.dataUri !== "string") throw new Error(imageErrorText(response));
    return response.data.dataUri;
  }

  async function stop() {
    if (!view || !activeTurn || !ownerReady || stopping || readOnlyHistory) return;
    setStopping(true);
    const response = await client.request("turn.interrupt", deviceId, { threadId: view.threadId, expectedTurnId: activeTurn.turnId });
    setStopping(false);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  async function respond(interaction: Interaction, decision: string) {
    if (!view || !ownerReady || working || readOnlyHistory) return;
    const raw = answers[interaction.interactionId] || {};
    const mapped = decision === "answer" ? answersForSubmission(interaction, raw) : null;
    if (decision === "answer" && !mapped) return;
    setWorking(true);
    const response = await client.request("interaction.respond", deviceId, { threadId: view.threadId, interactionId: interaction.interactionId, decision, ...(decision === "answer" ? { answers: mapped } : {}) });
    setWorking(false);
    if (response.outcome !== "accepted") setNotice(errorText(response));
  }

  function openThreadMenu(thread: Thread, left: number, top: number) {
    if (!threadContextActionsEnabled) return;
    setThreadMenu({ threadId: thread.threadId, left: Math.max(8, Math.min(left, window.innerWidth - 180)), top: Math.max(8, Math.min(top, window.innerHeight - 168)) });
  }

  function cancelThreadLongPress() {
    const pending = threadLongPress.current;
    if (pending) window.clearTimeout(pending.timer);
    threadLongPress.current = null;
  }

  function beginThreadLongPress(thread: Thread, event: ReactPointerEvent<HTMLButtonElement>) {
    if (!threadContextActionsEnabled || (event.button !== undefined && event.button !== 0)) return;
    cancelThreadLongPress();
    const pointerId = event.pointerId;
    const startX = event.clientX;
    const startY = event.clientY;
    const timer = window.setTimeout(() => {
      if (threadLongPress.current?.pointerId !== pointerId) return;
      suppressThreadClick.current = { threadId: thread.threadId, until: Date.now() + 800 };
      openThreadMenu(thread, startX, startY);
      threadLongPress.current = null;
    }, 500);
    threadLongPress.current = { threadId: thread.threadId, pointerId, startX, startY, timer };
  }

  function moveThreadLongPress(event: ReactPointerEvent<HTMLButtonElement>) {
    const pending = threadLongPress.current;
    if (!pending || pending.pointerId !== event.pointerId) return;
    if (Math.hypot(event.clientX - pending.startX, event.clientY - pending.startY) > 10) cancelThreadLongPress();
  }

  async function archiveThread(thread: Thread) {
    if (!threadArchiveEnabled || threadArchiveWorking || threadMetadataWorking === thread.threadId) return;
    setThreadMenu(null);
    setThreadArchiveWorking(thread.threadId);
    const targetDevice = deviceId;
    const targetAgentEpoch = agentEpochFor(targetDevice);
    const response = await client.request("thread.archive.update", targetDevice, { threadId: thread.threadId, archived: true });
    setThreadArchiveWorking("");
    if (targetDevice !== selection.current.deviceId || targetAgentEpoch !== agentEpochFor(targetDevice)) return;
    const operationId = response.data?.operationId;
    if (response.outcome !== "accepted" || typeof operationId !== "string" || !operationId) {
      setNotice(response.outcome === "accepted" ? "归档回执无法确认，会话列表已重新加载。" : errorText(response));
      void loadThreads(targetDevice, "", searchInputRef.current.trim());
      return;
    }
    setThreads(current => current.filter(candidate => candidate.threadId !== thread.threadId));
    if (selection.current.threadId === thread.threadId) {
      const subscriptionId = expectedSubscription.current;
      pendingSelect.current++;
      expectedSubscription.current = "";
      selection.current = { deviceId: targetDevice, threadId: "", view: null };
      setThreadId("");
      setView(null);
      setSyncingView(false);
      resetHistory();
      if (subscriptionId) void client.request("thread.unsubscribe", targetDevice, { subscriptionId });
    }
    setArchiveUndoToast({ threadId: thread.threadId, title: thread.title || "未命名会话", operationId });
  }

  function openRenameThreadDialog(thread: Thread) {
    setThreadMenu(null);
    setRenameThreadError("");
    setRenameThreadDialog({ threadId: thread.threadId, name: thread.title || "" });
  }

  function applyVerifiedThreadMetadata(thread: Thread) {
    setThreads(current => {
      const next = current.map(candidate => candidate.threadId === thread.threadId
        ? { ...candidate, title: thread.title, isPinned: thread.isPinned }
        : candidate);
      const identity = threadListIdentity(deviceId, searchInputRef.current.trim());
      if (identity) threadListCache.current.set(identity, { threads: next, cursor });
      return next;
    });
    const historyIdentity = historyCacheIdentity(deviceId, thread.threadId);
    const cached = historyIdentity ? historyCache.current.get(historyIdentity) : null;
    if (historyIdentity && cached?.thread) {
      historyCache.current.set(historyIdentity, { ...cached, thread: { ...cached.thread, title: thread.title, isPinned: thread.isPinned } });
    }
    setView(current => {
      if (!current || current.threadId !== thread.threadId) return current;
      const next = { ...current, thread: { ...current.thread, title: thread.title, isPinned: thread.isPinned } };
      selection.current.view = next;
      return next;
    });
  }

  async function renameThread() {
    const dialog = renameThreadDialog;
    const name = dialog?.name.trim() || "";
    if (!dialog || !threadRenameEnabled || threadMetadataWorking || threadArchiveWorking === dialog.threadId || !validThreadName(name)) {
      if (dialog) setRenameThreadError("名称需为 1–200 个字符，且不能包含控制字符。");
      return;
    }
    setThreadMetadataWorking(dialog.threadId);
    setRenameThreadError("");
    const targetDevice = deviceId;
    const targetAgentEpoch = agentEpochFor(targetDevice);
    const response = await client.request("thread.name.update", targetDevice, { threadId: dialog.threadId, name });
    setThreadMetadataWorking("");
    if (targetDevice !== selection.current.deviceId || targetAgentEpoch !== agentEpochFor(targetDevice)) return;
    const candidate = response.data?.thread as Thread | undefined;
    if (response.outcome === "accepted" && candidate?.threadId === dialog.threadId && candidate.title === name) {
      markThreadRead(targetDevice, candidate);
      applyVerifiedThreadMetadata(candidate);
      setRenameThreadDialog(null);
      return;
    }
    if (response.outcome === "unknown") {
      setRenameThreadDialog(null);
      setNotice("修改结果不确定，请检查会话列表。");
      void loadThreads(targetDevice, "", searchInputRef.current.trim(), true);
      return;
    }
    setRenameThreadError(response.outcome === "accepted" ? "修改回执无法确认，请重试。" : errorText(response));
  }

  async function pinThread(thread: Thread) {
    if (!threadPinEnabled || threadMetadataWorking || threadArchiveWorking === thread.threadId) return;
    setThreadMenu(null);
    setThreadMetadataWorking(thread.threadId);
    const pinned = thread.isPinned !== true;
    const targetDevice = deviceId;
    const targetAgentEpoch = agentEpochFor(targetDevice);
    const response = await client.request("thread.pin.update", targetDevice, { threadId: thread.threadId, pinned });
    setThreadMetadataWorking("");
    if (targetDevice !== selection.current.deviceId || targetAgentEpoch !== agentEpochFor(targetDevice)) return;
    const candidate = response.data?.thread as Thread | undefined;
    if (response.outcome === "accepted" && candidate?.threadId === thread.threadId && candidate.isPinned === pinned) {
      markThreadRead(targetDevice, candidate);
      applyVerifiedThreadMetadata(candidate);
      if (!pinned) {
        const group = groupThreadsByProject([candidate])[0];
        if (group) setExpandedProjectKeys(current => new Set(current).add(group.key));
      }
      void loadThreads(targetDevice, "", searchInputRef.current.trim(), true);
      return;
    }
    setNotice(response.outcome === "accepted" ? "置顶回执无法确认，会话列表已重新加载。" : response.outcome === "unknown" ? "置顶结果不确定，请检查会话列表。" : errorText(response));
    if (response.outcome === "unknown" || response.outcome === "accepted") void loadThreads(targetDevice, "", searchInputRef.current.trim(), true);
  }

  async function undoArchive() {
    const toast = archiveUndoToast;
    if (!toast || !device?.capabilities.threadArchiveUndo || threadArchiveWorking) return;
    setThreadArchiveWorking(toast.threadId);
    const response = await client.request("thread.archive.undo", deviceId, { threadId: toast.threadId, operationId: toast.operationId });
    setThreadArchiveWorking("");
    if (response.outcome !== "accepted") {
      setNotice(errorText(response));
      return;
    }
    setArchiveUndoToast(null);
    await loadThreads(deviceId, "", searchInputRef.current.trim());
  }

  function renderThreadRow(thread: Thread) {
    const archiveAllowed = selection.current.threadId === thread.threadId && view?.threadId === thread.threadId && view.thread.runtime === "idle";
    return <button key={thread.threadId} type="button" data-thread-id={thread.threadId} className={`thread-row ${threadId === thread.threadId ? "selected" : ""}`}
      onClick={() => { const suppressed = suppressThreadClick.current.threadId === thread.threadId && Date.now() <= suppressThreadClick.current.until; suppressThreadClick.current = { threadId: "", until: 0 }; if (!suppressed) void selectThread(thread.threadId); }}
      onContextMenu={event => { event.preventDefault(); openThreadMenu(thread, event.clientX, event.clientY); }}
      onKeyDown={event => { if ((event.shiftKey && event.key === "F10") || event.key === "ContextMenu") { event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); openThreadMenu(thread, rect.left + 16, rect.top + 16); } }}
      onPointerDown={event => beginThreadLongPress(thread, event)} onPointerMove={moveThreadLongPress} onPointerUp={cancelThreadLongPress} onPointerCancel={cancelThreadLongPress}
      data-archive-allowed={archiveAllowed ? "true" : "false"}>
      <span className="thread-title-line"><span className="thread-title">{thread.title || "未命名会话"}</span>{unreadThreadIDs.has(thread.threadId) && <span className="thread-unread" role="status" aria-label="有新结果" />}</span>
      {searchInput.trim() && thread.searchSnippet && <span className="thread-snippet">{thread.searchSnippet}</span>}
      <span className="thread-date">{new Date(thread.updatedAt).toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" })}</span>
    </button>;
  }

  const accessHero = <div><div className="connect-hero"><ArielLogo size="clamp(128px, 12vw, 160px)" variant="color" decorative priority className="connect-logo" /><div className="connect-hero-copy"><span className="eyebrow">PRIVATE ACCESS</span><h1>Agent 联络中继器</h1></div></div><p>{webAuthMode === "passkey" ? "使用此设备上的 Passkey 安全登录。会话凭据仅保存在受保护的浏览器 Cookie 中。" : "输入 6 位连接码。连接后，同一标签页刷新会自动恢复；连接码不会存入浏览器。"}</p></div>;
  const accessPanel = webAuthMode === "passkey" ? <section className="connect-panel" aria-label="Passkey 登录">{accessHero}
    {!passkeyAuth.loaded ? <div className="auth-loading" role="status">正在检查登录状态…</div> : <form onSubmit={event => { event.preventDefault(); void authenticateWithPasskey(); }}>
      {passkeyAuth.enrollmentRequired && <><label htmlFor="setup-token">首次设置密钥</label><input id="setup-token" type="password" value={setupToken} onChange={event => setSetupToken(event.target.value)} minLength={32} autoComplete="off" placeholder="输入部署时配置的 setup token" required /></>}
      <button className="primary passkey-action" type="submit" disabled={passkeyBusy || status === "connecting" || (passkeyAuth.enrollmentRequired && setupToken.length < 32)}>{passkeyBusy || status === "connecting" ? "正在处理…" : passkeyAuth.enrollmentRequired ? "创建 Passkey" : "使用 Passkey 登录"}</button>
      {passkeyAuth.error && <p role="alert" className="connect-error">{passkeyAuth.error}</p>}
      <small>{passkeyAuth.enrollmentRequired ? "首次登记成功后，请从云端部署 Secret 中移除 setup token。" : "Passkey 会校验当前 HTTPS 域名，不使用共享密码。"}</small>
    </form>}
  </section> : phonePairing ? <PhonePairingPanel pairing={phonePairing} origin={window.location.origin} onConfirm={confirmPhonePairing} onCancel={cancelPhonePairing} /> : <section className="connect-panel" aria-label="连接 Relay">{accessHero}<form onSubmit={event => { event.preventDefault(); if (isWebPIN(token)) { savedSessionAttempt.current = false; setSessionExpired(false); client.connect(token); } }}><label htmlFor="token">6 位连接码</label><div className="connect-row"><input id="token" type="password" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={token} onChange={event => setToken(event.target.value)} autoComplete="off" placeholder="输入 6 位数字" required /><button className="primary" type="submit" disabled={!isWebPIN(token)}>连接 <span aria-hidden="true">↗</span></button></div>{status === "invalid" && <p role="alert" className="connect-error">{sessionExpired ? "保存的会话已失效，请重新输入连接码。" : "连接码错误或 Relay 已锁定；累计 10 次错误后需重启 Relay。"}</p>}<small>仅建议在可信局域网使用。HTTP/WS 连接未加密。</small></form></section>;

  return <div className={`app-shell ${status === "ready" ? "connected" : ""}`} style={visualViewportHeight === null ? undefined : { height: visualViewportHeight }}>
    <header className="masthead">
      <div className="brand"><ArielLogo size={29} variant="micro" tone="white" decorative className="brand-logo" /><span>Ariel</span><small>Codex 随身工作台</small></div>
      <div className="mast-actions"><span className={`connection ${status}`}><span className="status-dot" />{connectionLabel}</span></div>
    </header>
    {newThreadOpen && <div className="thread-create-backdrop" role="presentation">
      <section className="thread-create-dialog" role="dialog" aria-modal="true" aria-labelledby="thread-create-title">
        <button className="thread-create-close icon-button icon-control" type="button" aria-label="关闭新建会话" data-tooltip="关闭" onClick={closeNewThreadDialog} disabled={creatingThread}><CloseIcon /></button>
        <span className="eyebrow">NEW CODEX SESSION</span>
        <h2 id="thread-create-title">新建会话</h2>
        <p>选择一个已有项目，或输入这台电脑上的绝对目录。会先建立本地草稿，发送第一条消息后才创建 Codex 会话。</p>
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
    {renameThreadDialog && <div className="thread-create-backdrop thread-rename-backdrop" role="presentation">
      <section className="thread-create-dialog thread-rename-dialog" role="dialog" aria-modal="true" aria-labelledby="thread-rename-title">
        <button className="thread-create-close icon-button icon-control" type="button" aria-label="关闭重命名" data-tooltip="关闭" onClick={() => setRenameThreadDialog(null)} disabled={!!threadMetadataWorking}><CloseIcon /></button>
        <span className="eyebrow">CODEX SESSION</span>
        <h2 id="thread-rename-title">修改会话名称</h2>
        <label htmlFor="thread-rename-name">会话名称</label>
        <input ref={renameThreadInput} id="thread-rename-name" aria-label="会话名称" value={renameThreadDialog.name} onChange={event => setRenameThreadDialog(current => current ? { ...current, name: event.target.value } : current)} onKeyDown={event => { if (event.key === "Enter" && !event.nativeEvent.isComposing) { event.preventDefault(); void renameThread(); } }} disabled={!!threadMetadataWorking} />
        {renameThreadError && <p className="thread-create-error" role="alert">{renameThreadError}</p>}
        <div className="thread-create-actions"><button className="secondary" type="button" onClick={() => setRenameThreadDialog(null)} disabled={!!threadMetadataWorking}>取消</button><button className="primary" type="button" aria-label="保存名称" onClick={() => void renameThread()} disabled={!!threadMetadataWorking || !validThreadName(renameThreadDialog.name)}>{threadMetadataWorking ? "正在保存…" : "保存"}</button></div>
      </section>
    </div>}
    {(status === "invalid" || (status !== "ready" && !view && !localThreadDraft)) && accessPanel}
    <div className="workspace">
      {showList && <button className="sidebar-backdrop" type="button" aria-label="关闭会话列表遮罩" onClick={() => setShowList(false)} />}
      <aside id="session-sidebar" className={`sidebar ${showList ? "open" : ""}`} aria-label="会话列表">
        <div className="sidebar-toolbar"><div className="brand"><ArielLogo size={24} variant="micro" tone="white" decorative className="brand-logo" /><span>Ariel</span><span className={`connection ${status}`} aria-label={connectionLabel}><span className="status-dot" aria-hidden="true" /><span className="sr-only">{connectionLabel}</span></span></div><div className="sidebar-toolbar-actions">{threadCreateEnabled && <button className="icon-button icon-control" type="button" aria-label="新建会话" data-tooltip="新建会话" onClick={openNewThreadDialog}><PlusIcon /></button>}{webAuthMode === "pin" ? <button className="icon-button icon-control" type="button" aria-label="手机扫码登录" data-tooltip="手机扫码登录" onClick={() => void openPairingInvite()}><PairIcon /></button> : <button className="icon-button icon-control" type="button" aria-label="添加 Passkey" data-tooltip="添加 Passkey" onClick={() => void addPasskey()} disabled={passkeyBusy}><PasskeyIcon /></button>}<button className="icon-button icon-control" aria-label="刷新会话" data-tooltip="刷新会话" onClick={() => void loadThreads(deviceId, "", searchInputRef.current.trim(), true)} disabled={!deviceId || searchInput.trim() !== searchTerm}><RefreshIcon /></button><button className="sidebar-disconnect icon-button icon-control" aria-label={webAuthMode === "passkey" ? "退出" : "断开"} data-tooltip={webAuthMode === "passkey" ? "退出" : "断开"} onClick={disconnect}><DisconnectIcon /></button><button className="icon-button icon-control mobile-close" aria-label="关闭会话列表" data-tooltip="关闭" onClick={() => setShowList(false)}><CloseIcon /></button></div></div>
        <div className="sidebar-device-row"><span className={`status-dot ${device?.agentOnline ? "online" : ""}`} aria-hidden="true" /><select id="device" aria-label="设备" aria-describedby="sidebar-device-status" value={deviceId} onChange={e => setDeviceId(e.target.value)} disabled={status !== "ready"}><option value="">{devices.length ? "选择设备" : "暂无在线设备"}</option>{devices.map(d => <option key={d.deviceId} value={d.deviceId}>{d.deviceName}</option>)}</select><span id="sidebar-device-status" className="sr-only">{device ? `${device.deviceName}，${device.agentOnline ? "Agent 在线" : "Agent 离线"}，${device.codexReady ? "Codex 就绪" : mock ? "Mock 演示" : "Codex 未就绪"}` : "未选择设备"}</span></div>
        <div className="sidebar-search"><span className="search-glyph"><SearchIcon /></span><input type="search" aria-label="搜索会话" placeholder="搜索会话与消息" value={searchInput} maxLength={128} onChange={e => updateSearch(e.target.value)} disabled={status !== "ready" || !deviceId} />{searchInput && <button className="icon-control" type="button" aria-label="清空搜索" data-tooltip="清空搜索" onClick={() => updateSearch("")}><CloseIcon /></button>}</div>
        <div className="list-caption"><span>{searchInput.trim() ? "搜索结果" : "最近会话"}</span><span>{pinnedThreads.length > 0 ? `${pinnedThreads.length} 个置顶 · ` : ""}{threadGroups.length} 个项目 · {threads.length} 个会话</span></div>
        <div className="thread-list" onScroll={cancelThreadLongPress}>{pinnedThreads.length > 0 && <section className="pinned-group" aria-label="置顶会话"><header className="pinned-heading"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M9 3h6l-1 6 3 3v2H7v-2l3-3-1-6Zm3 11v7" /></svg><span>置顶</span><span className="project-count" aria-hidden="true">{pinnedThreads.length}</span></header><div className="pinned-threads">{pinnedThreads.map(renderThreadRow)}</div></section>}{threadGroups.map((group, index) => {
          const expanded = expandedProjectKeys.has(group.key);
          const unread = unreadProjectIDs.has(group.key);
          const projectThreadsID = `project-threads-${index}`;
          return <section className={`project-group ${expanded ? "expanded" : "collapsed"}`} aria-label={`项目 ${group.name}`} key={group.key}>
            <header className="project-heading"><button className="project-toggle" type="button" aria-expanded={expanded} aria-controls={projectThreadsID} aria-label={`${expanded ? "收起" : "展开"}项目 ${group.name}，${group.path}`} onClick={() => toggleProjectGroup(group.key)}><span className={`project-chevron ${expanded ? "expanded" : ""}`} aria-hidden="true">›</span><span className="project-copy"><span className="project-name-line"><span className="project-name">{group.name}</span>{unread && <span className="thread-unread project-unread" role="status" aria-label="项目有新结果" />}</span><span className="project-path" title={group.path}>{group.path}</span></span><span className="project-count" aria-hidden="true">{group.threads.length}</span></button></header>
            <div id={projectThreadsID} className="project-threads" hidden={!expanded}>{expanded && group.threads.map(renderThreadRow)}</div>
          </section>;
        })}{listLoading && <p className="list-state" role="status">{searchInput.trim() ? "正在搜索会话…" : "正在加载会话…"}</p>}{listError && <p className="list-state list-error" role="alert">{listError}</p>}{!listLoading && !listError && searchInput.trim() && threads.length === 0 && status === "ready" && <p className="list-state">没有找到匹配会话</p>}</div>
        {cursor && <button className="load-more" onClick={() => void loadThreads(deviceId, cursor, searchInputRef.current.trim())} disabled={listLoading}>加载更多 →</button>}
        <div className="sidebar-foot">{mock ? "MOCK SESSION · 非真实 Codex 历史" : "原始会话 · 不创建远程副本"}</div>
      </aside>
      <main className="conversation">
        <div className={`conversation-head ${historyChromeCollapsed ? "history-collapsed" : ""}`} data-history-collapsed={historyChromeCollapsed}>
          <button className="mobile-list icon-control" aria-label={`打开会话列表，${connectionLabel}`} data-tooltip="会话列表" aria-expanded={showList} aria-controls="session-sidebar" onClick={() => { setShowList(true); void refreshUnreadThreads(true); }}><MenuIcon /><span className={`status-dot ${status === "ready" ? "online" : ""}`} aria-hidden="true" /></button>
          <div className="conversation-title"><span className="eyebrow">{mock ? "MOCK DEMO" : "CODEX SESSION"}</span><h2>{localThreadDraft ? "未命名会话" : view ? view.thread.title || "未命名会话" : selectedThread ? selectedThread.title || "未命名会话" : "选择一个会话"}</h2><span className="head-path">{localThreadDraft?.cwd || view?.thread.cwd || selectedThread?.cwd || "从左侧选择历史会话，接着工作。"}</span></div>
          <div className="head-right">{mock && <span className="mock-badge">模拟环境</span>}{view && <span className="runtime">{view.thread.runtime === "inProgress" ? "运行中" : view.thread.runtime === "idle" ? "待命" : view.thread.runtime === "notLoaded" ? "加载中" : "状态未知"}</span>}</div>
          {currentPermissions && <div className="permission-info" ref={permissionInfoRef}><button className={`permission-info-button icon-control ${permissionWarning ? "danger" : ""}`} type="button" aria-label={currentPermissions.label} data-tooltip="权限详情" aria-expanded={permissionInfoOpen} aria-controls="desktop-permission-details" onClick={() => setPermissionInfoOpen(open => !open)}><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 10.5v5"/><path d="M12 7.5h.01"/></svg></button>{permissionInfoOpen && <div id="desktop-permission-details" className="permission-popover" role="dialog" aria-label="当前 Desktop 权限详情"><div className="permission-popover-title">当前 Desktop 权限</div><div>{currentPermissions.label}</div>{currentPermissions.warning && <p>{currentPermissions.warning}</p>}</div>}</div>}
        </div>
        {currentPermissions && <section className={`permission-strip ${currentPermissions.warning ? "danger" : ""}`} aria-label="当前 Desktop 权限" role={currentPermissions.warning ? "alert" : "status"}><span>{currentPermissions.label}</span>{currentPermissions.warning && <span className="permission-note">{currentPermissions.warning}</span>}</section>}
        {readOnlyHistory && view && <div className="readonly-banner" role="status">历史只读 · 当前 Desktop 状态未确认，发送、停止和审批已禁用。</div>}
        {syncingView && view && !readOnlyHistory && !connectionResume.current && <div className="readonly-banner" role="status">正在同步最新状态 · 当前内容可继续阅读和编辑草稿，操作暂时禁用。</div>}
        <div className="transcript" ref={transcriptRef} onScroll={onTranscriptScroll} onWheel={onTranscriptWheel} onTouchStart={onTranscriptTouchStart} onTouchMove={onTranscriptTouchMove} onTouchEnd={onTranscriptTouchEnd} onTouchCancel={onTranscriptTouchEnd} aria-live="polite">
          {!view && <div className="empty"><div className="empty-symbol"><ArielLogo size={50} variant="micro" tone="white" decorative className="session-loading-logo" /></div><h3>{localThreadDraft ? "新会话草稿" : blockedSelection.current === `${deviceId}\u0000${threadId}` ? "会话状态无法确认" : threadId ? "正在同步会话…" : "从这里接续"}</h3><p>{localThreadDraft ? "发送第一条消息后创建 Codex 会话。" : blockedSelection.current === `${deviceId}\u0000${threadId}` ? "远程操作已暂停。请稍后手动重新选择会话。" : threadId ? "等待电脑端加载原始历史。" : "选一个会话，历史、运行状态与需要你决定的问题会出现在这里。"}</p></div>}
          {view?.thread.historyComplete === false && !history.exhausted && <div className="history-control"><button className="history-action" type="button" onClick={() => void loadOlder()} disabled={historyLoading}>{historyLoading ? "正在加载更早消息…" : "加载更早消息"}</button></div>}
          {displayedTurns.slice(0, history.older.length).map(turn => <ConversationTurn key={`${view?.threadId}:${turn.turnId}`} turn={turn} fileChanges={fileChanges[`${view?.threadId}\u0000${turn.turnId}`]} onOpenFile={(change,mode)=>void openChangedFile(turn.turnId,change,mode)} loading={itemLoading === turn.turnId} onLoadOlderItems={turn => void loadOlderItems(turn)} onLoadImage={(itemId, index) => loadImage(turn.turnId, itemId, index)} active={ownerReady && view?.thread.runtime === "inProgress" && view.thread.pendingInteractions.length === 0 && activeTurn?.turnId === turn.turnId} />)}
          {history.gap && <div className="history-gap">中间消息已从当前浏览窗口释放 <button type="button" onClick={resetHistory}>回到最新</button></div>}
          {displayedTurns.slice(history.older.length).map(turn => <ConversationTurn key={`${view?.threadId}:${turn.turnId}`} turn={turn} fileChanges={fileChanges[`${view?.threadId}\u0000${turn.turnId}`]} onOpenFile={(change,mode)=>void openChangedFile(turn.turnId,change,mode)} loading={itemLoading === turn.turnId} onLoadOlderItems={turn => void loadOlderItems(turn)} onLoadImage={(itemId, index) => loadImage(turn.turnId, itemId, index)} active={ownerReady && view?.thread.runtime === "inProgress" && view.thread.pendingInteractions.length === 0 && activeTurn?.turnId === turn.turnId} />)}
          {view?.thread.pendingInteractions.map(card => <InteractionCard key={card.interactionId} card={card} values={answers[card.interactionId] || {}} onChange={(id, value) => setAnswers(all => ({ ...all, [card.interactionId]: { ...all[card.interactionId], [id]: value } }))} onRespond={decision => void respond(card, decision)} disabled={working || !ownerReady} />)}
        </div>
        {(showReturnToLatest || historyChromeCollapsed) && view && <div className={`return-latest-bar ${historyChromeCollapsed ? "history-overlay" : ""}`}><button className="icon-control" type="button" aria-label={showReturnToLatest ? "回到最新" : "恢复输入区并回到最新"} data-tooltip={showReturnToLatest ? "回到最新" : "恢复并回到最新"} onClick={returnToLatest}><ReturnToLatestIcon /></button></div>}
        <div className={`composer-wrap ${historyChromeCollapsed ? "history-collapsed" : ""}`} data-history-collapsed={historyChromeCollapsed}><div className="composer-stack">{notice && <div className="notice" role="alert"><span>!</span>{notice}<button className="icon-control" aria-label="关闭提示" data-tooltip="关闭提示" onClick={() => setNotice("")}><CloseIcon /></button></div>}
          {recoveringDeviceId === deviceId && view && <div className="readonly-banner" role="status">Desktop Agent 正在重连 · 当前显示最后确认的会话内容，操作暂时禁用。</div>}
          {queuedMessages.length > 0 && <section className="queue-panel" aria-label="排队的后续输入">
            {queuePaused && <div className="queue-head"><span>后续输入已暂停</span><small>开始下一轮前可继续调整</small></div>}
            <span className="sr-only">共 {queuedMessages.length} 条，最上方优先执行。</span>
            <ol className="queue-list">{renderedQueuedMessages.map((item, index) => {
              const busy = !!queueWorking || working || !!draggingQueueId;
              const summary = item.text.trim() || (item.images.length ? `${item.images.length} 张截图` : "无法预览的输入");
              return <li className={`queue-item ${editingQueueId === item.queueId ? "editing" : ""} ${draggingQueueId === item.queueId ? "dragging" : ""}`} key={item.queueId} data-queue-id={item.queueId}>
                <button className="queue-handle" type="button" aria-label={`拖拽排序：${summary}`} title="拖拽排序；也可用上下方向键" onPointerDown={event => beginQueueDrag(item, event)} onKeyDown={event => queueHandleKeyDown(index, event)} disabled={!ownerReady || !!queueWorking || working}><QueueHandleIcon /></button>
                <div className="queue-content"><span className="queue-text">{summary}</span>{item.images.length > 0 && item.text.trim() && <small>{item.images.length} 张截图</small>}{!item.editable && <small>此输入只能删除或调序</small>}</div>
                <div className="queue-actions">
                  {item.editable && <button className="queue-guide-action icon-control" type="button" aria-label={`引导：${summary}`} data-tooltip="插入当前" onClick={() => steerQueuedMessage(item)} disabled={!ownerReady || busy || !activeTurn || !!view?.thread.pendingInteractions.length}><QueueGuideIcon /></button>}
                  <button className="queue-icon-action icon-control" type="button" aria-label={`删除排队消息：${summary}`} data-tooltip="删除" onClick={() => deleteQueuedMessage(item)} disabled={!ownerReady || busy}><QueueTrashIcon /></button>
                  <div className="queue-more-wrap">
                    <button className="queue-more-action icon-control" type="button" aria-label={`更多选项：${summary}`} data-tooltip="更多选项" aria-haspopup="menu" aria-expanded={queueMenuId === item.queueId} onClick={event => toggleQueueMenu(item, event)} disabled={!ownerReady || busy}><MoreIcon /></button>
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
          <FileQuoteChips quotes={fileQuotes} onRemove={index=>setFileQuotes(current=>current.filter((_,position)=>position!==index))}/>
          {screenshots.length > 0 && <div className="screenshot-drafts" aria-label="待发送截图">{screenshots.map((image, index) => <div className="screenshot-draft" key={`${image.name}:${index}`}><img src={image.dataUri} alt={image.name} /><button className="icon-control" type="button" aria-label={`移除截图：${image.name}`} data-tooltip="移除截图" onClick={() => setScreenshots(current => current.filter((_, position) => position !== index))}><CloseIcon /></button></div>)}</div>}
          <div className="composer"><textarea ref={composerInputRef} aria-label="发送消息" enterKeyHint="enter" placeholder={localThreadDraft ? "发送第一条消息，创建 Codex 会话…" : view ? editingQueueId ? "编辑排队消息…" : view.thread.runtime === "inProgress" ? queueEnabled ? "继续输入，发送后加入队列…" : "Codex 正在运行；你可以先写草稿…" : "给 Codex 发消息…" : "选择会话后开始输入…"} value={draft} onChange={e => setDraft(e.target.value)} onKeyDown={e => { if (window.innerWidth > mobileViewportMaxWidth && e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(); } }} disabled={(!view && !localThreadDraft) || readOnlyHistory} rows={1} />
            <div className="composer-actions"><span>Enter 发送 · Shift+Enter 换行</span><div>
              <input ref={screenshotInputRef} className="screenshot-file" type="file" aria-label="附加截图" accept="image/png,image/jpeg" multiple onChange={e => void addScreenshots(e.target.files)} disabled={(!view && !localThreadDraft) || readOnlyHistory || working} />
              <button className="attach-button icon-control" type="button" aria-label="选择截图" data-tooltip="附加截图" onClick={() => screenshotInputRef.current?.click()} disabled={(!view && !localThreadDraft) || readOnlyHistory || working}><PlusIcon /></button>
              {editingQueueId && <button className="queue-cancel-button icon-control" type="button" aria-label="取消编辑" data-tooltip="取消编辑" onClick={cancelQueueEdit} disabled={working}><CloseIcon /></button>}
              {activeTurn && <button className="stop-button icon-control" type="button" aria-label="停止" data-tooltip="停止" onClick={() => void stop()} disabled={stopping || !ownerReady || readOnlyHistory}><StopIcon /></button>}
              <button className={`primary send-button icon-control ${ownerRecoveryLoading ? "loading" : ""}`} type="button" aria-label={displayedSendAction} data-tooltip={displayedSendAction} onClick={() => void send()} disabled={ownerRecoveryLoading || readOnlyHistory || (localThreadDraft ? !composerReady || working || (!draft.trim() && screenshots.length === 0) : !canSend(view?.thread || null, ownerReady, working || !!queueWorking, draft, screenshots.length + fileQuotes.length, queueEnabled))}>{ownerRecoveryLoading ? <SendLoadingIcon /> : <SendIcon />}</button>
            </div></div></div></div></div>
      </main>
    </div>
    {threadMenu && (() => {
      const target = threads.find(candidate => candidate.threadId === threadMenu.threadId);
      if (!target) return null;
      const archiveAllowed = ownerReady && selection.current.threadId === target.threadId && view?.threadId === target.threadId && view.thread.runtime === "idle";
      const targetWorking = threadMetadataWorking === target.threadId || threadArchiveWorking === target.threadId;
      return createPortal(<div className="thread-context-menu" role="menu" aria-label={`会话操作：${target.title || "未命名会话"}`} style={{ left: threadMenu.left, top: threadMenu.top }}>
        {threadRenameEnabled && <button ref={threadMenuFirstItem} type="button" role="menuitem" onClick={() => openRenameThreadDialog(target)} disabled={targetWorking}>重命名</button>}
        {threadPinEnabled && <button ref={!threadRenameEnabled ? threadMenuFirstItem : undefined} type="button" role="menuitem" onClick={() => void pinThread(target)} disabled={targetWorking}>{target.isPinned ? "取消置顶" : "置顶"}</button>}
        {threadArchiveEnabled && <button ref={!threadRenameEnabled && !threadPinEnabled ? threadMenuFirstItem : undefined} type="button" role="menuitem" onClick={() => void archiveThread(target)} disabled={!archiveAllowed || targetWorking}>归档</button>}
        {threadArchiveEnabled && !archiveAllowed && <small>会话处理完成后可归档</small>}
      </div>, document.body);
    })()}
    {archiveUndoToast && <div className="archive-toast" role="status"><span>已归档「{archiveUndoToast.title}」；派生会话也可能一并归档。</span><button type="button" aria-label="撤销归档" onClick={() => void undoArchive()} disabled={!!threadArchiveWorking}>撤销</button></div>}
    {pairingInvite && <PairingDialog invite={pairingInvite} seconds={pairingSeconds} onClose={closePairingInvite} onRegenerate={() => void openPairingInvite()} />}
		{openFile && <FileViewer view={openFile} onClose={closeFile} onModeChange={changeFileMode} onQuote={quote=>{setFileQuotes(current=>current.length>=5?current:[...current,quote]);closeFile();window.setTimeout(()=>composerInputRef.current?.focus(),0);}}/>}
  </div>;
}

function ConversationTurn({ turn, fileChanges, onOpenFile, loading, onLoadOlderItems, onLoadImage, active }: { turn: Turn; fileChanges?: FileChange[]; onOpenFile: (change:FileChange,mode:FileViewMode)=>void; loading: boolean; onLoadOlderItems: (turn: Turn) => void; onLoadImage: (itemId: string, index: number) => Promise<string>; active: boolean }) {
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
		const projected = item.role === "user" ? projectCanonicalFileContexts(item.text) : { text:item.text, quotes:[] };
        const markdownImages = item.images?.filter(image => image.kind === "markdown") || [];
        const nativeImages = item.images?.filter(image => image.kind === "native") || [];
        return <article className={`message ${item.role}`} aria-label={item.role === "user" ? "你" : item.role === "assistant" ? "Codex" : "系统"} key={item.itemId} data-item-id={item.itemId}><div className="message-body"><HistoricalFileQuotes quotes={projected.quotes}/><div className="message-text">{item.role === "system" ? projected.text : <ConversationMarkdown text={projected.text} renderImage={(source, alt) => { const ref = markdownImages.find(image => image.source === source); return ref ? <ConversationImage alt={alt || ref.alt} autoLoad={false} load={() => onLoadImage(item.itemId, ref.index)} /> : null; }} />}</div>{nativeImages.map(image => <ConversationImage key={image.index} alt={image.alt} load={() => onLoadImage(item.itemId, image.index)} />)}</div></article>;
      }
      if (!activityExpanded && part !== firstActivity) return null;
      return <section className="activity-group" key={part.items[0].itemId} data-item-id={part === firstActivity ? `${turn.turnId}:activity-control` : undefined}>
        {part === firstActivity && <button className={`activity-toggle ${activityExpanded ? "open" : ""}`} type="button" aria-label={activityLabel} title={activityLabel} aria-expanded={activityExpanded} onClick={() => setActivityExpanded(open => !open)}>{activityToggleText}</button>}
        {activityExpanded && <div className="activity-list">{part.firstLoaded && <p className="activity-note">此回合还有更早内容，可在上方按需加载。</p>}{part.items.map(item => <div className="activity-item" key={item.itemId} data-item-id={item.itemId}><div className="activity-item-head"><span>{item.activity!.label}</span><span>{activityStatusText(item.activity!.status)}</span></div><div className="activity-kind">{item.activity!.kind}</div>{item.activity!.details ? <pre className="activity-details">{item.activity!.details}</pre> : <p className="activity-note">当前历史没有更多详情。</p>}{item.activity!.truncated && <p className="activity-note">详情已截断；完整内容请在原 Codex Desktop 查看。</p>}</div>)}</div>}
      </section>;
    })}
		{turn.status === "completed" && fileChanges && <TurnFileChanges changes={fileChanges} onOpen={onOpenFile}/>}
    {active && <div className="agent-progress" role="status">{processing ? "处理中…" : "思考中…"}</div>}
  </section>;
}

export function InteractionCard({ card, values, onChange, onRespond, disabled }: { card: Interaction; values: Record<string, string>; onChange: (id: string, value: string) => void; onRespond: (decision: string) => void; disabled: boolean }) {
  return <section className="interaction-card"><span className="eyebrow">NEEDS YOUR INPUT</span><h3>{card.kind === "command_approval" ? "等待命令审批" : card.kind === "file_approval" ? "等待文件变更审批" : card.kind === "permission_request" ? "等待权限请求" : card.kind === "user_input" ? "Codex 有一个问题" : "暂不支持的交互"}</h3><p>{card.prompt}</p>{card.questions?.map(q => <label key={q.id} className="question">{q.question}<input list={`options-${card.interactionId}-${q.id}`} value={values[q.id] || ""} onChange={e => onChange(q.id, e.target.value)} placeholder={q.options?.length ? "选择建议或自行输入" : "输入回答"} /><datalist id={`options-${card.interactionId}-${q.id}`}>{q.options?.map(o => <option key={o} value={o} />)}</datalist></label>)}<div className="interaction-actions">{card.availableDecisions.map(decision => <button key={decision} className={decision === "accept_once" || decision === "answer" ? "primary" : "secondary"} disabled={disabled || (decision === "answer" && !answersForSubmission(card, values))} onClick={() => onRespond(decision)}>{decision === "accept_once" && card.kind === "permission_request" ? "仅本轮按原请求授权" : ({ accept_once: "仅本次允许", deny: "拒绝", deny_and_stop: "拒绝并停止", answer: "提交回答" } as Record<string, string>)[decision]}</button>)}</div></section>;
}
