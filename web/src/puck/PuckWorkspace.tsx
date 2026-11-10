import { Component, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { readScreenshotFiles, type ScreenshotDraft } from "../screenshots";
import { useImeEnterGuard } from "../ime-enter";
import { avatarColorHex, avatarColors, avatarImage, avatarShapes, canonicalShape, deriveAvatar, nearestAvatarColor, PuckAvatar, type AvatarColor, type AvatarDesign, type AvatarShape, type PuckState } from "./PuckAvatar";
import "./puck.css";

type PuckResponse = { outcome: string; data?: Record<string, unknown>; error?: { code?: string; message?: string } };
type Request = (method: string, target: string, params: Record<string, unknown>) => Promise<PuckResponse>;
type SubscribeEvents = (listener: (event: Record<string, unknown>) => void) => () => void;
type Device = { deviceId: string; name?: string; capabilities?: Record<string, boolean> };
type Assistant = { assistantId: string; name: string; avatar?: string; description?: string; brainDeviceId: string; status: string; permissionMode?: "full" | "ask"; unreadCount?: number; lastMessagePreview?: string };
type AuthorizationRule = { id: string; assistantId: string; actionType: string; deviceId: string; cwd: string; createdAt: string };
type Message = { id: string; assistantId?: string; authorAssistantId?: string; authorName?: string; role: string; kind: string; text: string; cardJSON?: string; createdAt?: string; workSummary?: string; reaction?: string; replyTo?: string };
type PuckGroup = { groupId: string; name: string; members: Array<{ assistantId: string; name: string }> };
const reactionChoices = ["👍", "❤️", "😂", "🎉", "👀"] as const;
const lastAssistantStorageKey = "ariel.puck.lastAssistantId.v1";
const latestScrollThreshold = 48;

function storedLastAssistant(): string {
  try { return window.localStorage.getItem(lastAssistantStorageKey)?.trim() || ""; } catch { return ""; }
}
function storeLastAssistant(assistantID: string) {
	try {
		if (assistantID) window.localStorage.setItem(lastAssistantStorageKey, assistantID);
		else window.localStorage.removeItem(lastAssistantStorageKey);
	} catch { /* preference storage is best-effort */ }
}

const puckErrorText: Record<string, string> = {
  RESYNC_REQUIRED: "实时连接暂时失效，请重试。",
  BRAIN_UNAVAILABLE: "助理暂时无法思考，请稍后重试。",
  BRAIN_HOST_OFFLINE: "助理所在主机暂时离线，请稍后重试。",
  DEVICE_OFFLINE: "目标设备当前离线，请稍后重试。",
  TIMEOUT: "请求超时，请重试。",
  OVERLOADED: "当前请求较多，请稍后重试。",
  OUTCOME_UNKNOWN: "操作结果暂时无法确认，请刷新后核对。",
};
function isRawErrorCode(value: string): boolean { return /^[A-Z][A-Z0-9_]+$/.test(value.trim()); }

function responseError(response: PuckResponse): string {
  const code = response.error?.code?.trim() || "";
  if (puckErrorText[code]) return puckErrorText[code];
  const message = response.error?.message?.trim() || "";
  return message && !isRawErrorCode(message) ? message : "请求暂时未完成，请重试。";
}
function friendlyAssistantText(role: string, text: string): string {
  if (role !== "assistant") return text;
  const code = text.trim();
  if (puckErrorText[code]) return puckErrorText[code];
  return /^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+$/.test(code) ? "操作暂时未完成，请重试。" : text;
}
function arrayData<T>(value: unknown): T[] { return Array.isArray(value) ? value as T[] : []; }
type QuestionOption = { value: string; label: string; description?: string };
type Question = { id: string; question: string; options: QuestionOption[]; allowCustom: boolean; multiSelect: boolean };
type QuestionAnswer = { questionId: string; values: string[]; custom?: string };
function questionOptions(value: unknown): QuestionOption[] {
  return arrayData<unknown>(value).flatMap(option => {
    if (typeof option === "string") return option ? [{ value: option, label: option }] : [];
    if (!option || typeof option !== "object" || Array.isArray(option)) return [];
    const record = option as Record<string, unknown>;
    const label = typeof record.label === "string" ? record.label.trim() : "";
    if (!label) return [];
    const value = typeof record.value === "string" && record.value.trim() ? record.value : label;
    const description = typeof record.description === "string" && record.description.trim() ? record.description.trim() : undefined;
    return [{ value, label, description }];
  });
}
function questionList(value: unknown): Question[] {
  return arrayData<unknown>(value).flatMap((entry, index) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) return [];
    const record = entry as Record<string, unknown>;
    const question = typeof record.question === "string" ? record.question.trim() : "";
    const options = questionOptions(record.options);
    const allowCustom = record.allowCustom === true || record.allowFreeText === true;
    if (!question || (options.length === 0 && !allowCustom)) return [];
    return [{ id: String(record.id || `question-${index + 1}`), question, options, allowCustom, multiSelect: record.multiSelect === true }];
  });
}
function parsedObject(value: string): Record<string, unknown> | undefined {
  try { const parsed = JSON.parse(value) as unknown; return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : undefined; } catch { return undefined; }
}
function normalizeQuestionData(message: Message): Record<string, unknown> & { questions: Question[] } {
  let data = cardData(message);
  let questions = questionList(data.questions);
  if (questions.length === 0 && typeof data.question === "string") questions = questionList([{ id: data.id || "question-1", question: data.question, options: data.options, allowCustom: data.allowCustom ?? data.allowFreeText, multiSelect: data.multiSelect }]);
  if (questions.length === 0) {
    const historical = parsedObject(message.text);
    if (historical) {
      data = { ...historical, ...data };
      questions = questionList(data.questions);
      if (questions.length === 0 && typeof data.question === "string") questions = questionList([{ id: data.id || "question-1", question: data.question, options: data.options, allowCustom: data.allowCustom ?? data.allowFreeText, multiSelect: data.multiSelect }]);
    }
  }
  if (questions.length === 0 && questionOptions(data.options).length > 0) questions = questionList([{ id: "question-1", question: message.text, options: data.options, allowCustom: data.allowCustom ?? data.allowFreeText, multiSelect: data.multiSelect }]);
  const fallbackID = message.id.startsWith("question-card:") ? message.id.slice("question-card:".length) : message.id;
  return { ...data, questionId: String(data.questionId || fallbackID), questions };
}
function normalizeMessage(value: Record<string, unknown>): Message {
  const role = String(value.role ?? value.Role ?? "assistant");
  const rawText = String(value.text ?? value.Text ?? "");
  const rawKind = String(value.kind ?? value.Kind ?? "text");
  const structuredJSON = role === "assistant" && ["question","authorization","share_result","task"].includes(rawKind) && rawText.trimStart().startsWith("{");
  const structuredFallback: Record<string,string> = { question:"助手发来了一个需要你决定的问题", authorization:"助手发来了一个授权请求", share_result:"助手分享了一项结果", task:"助手更新了一项任务" };
  const text = structuredJSON ? structuredFallback[rawKind] : friendlyAssistantText(role, rawText);
  return {
    id: String(value.id ?? value.ID ?? crypto.randomUUID()), assistantId: String(value.assistantId ?? value.AssistantID ?? "") || undefined, authorAssistantId: String(value.authorAssistantId ?? value.AuthorAssistantID ?? "") || undefined, authorName: String(value.authorName ?? value.AuthorName ?? "") || undefined, role,
    kind: text !== rawText && !structuredJSON ? "error" : rawKind, text,
    cardJSON: String(value.cardJSON ?? value.CardJSON ?? (structuredJSON ? rawText : "")), createdAt: String(value.createdAt ?? value.CreatedAt ?? ""), workSummary: String(value.workSummary ?? value.WorkSummary ?? ""), reaction: String(value.reaction ?? value.Reaction ?? ""), replyTo: String(value.replyTo ?? value.ReplyTo ?? "") || undefined,
  };
}
function historyMessages(value: unknown, assistantID: string): Message[] {
  return arrayData<Record<string, unknown>>(value).map(normalizeMessage).filter(message => !message.assistantId || message.assistantId === assistantID);
}
function workData(message: Message): Record<string, unknown> { try { return message.workSummary ? JSON.parse(message.workSummary) as Record<string, unknown> : {}; } catch { return {}; } }
function workLabel(message: Message, now: number): string {
	const data = workData(message); const elapsed = Number(data.elapsedMs);
	if (Number.isFinite(elapsed)) return `工作了 ${Math.max(0, Math.ceil(elapsed / 1000))} 秒 · 执行 ${Math.max(0, Number(data.steps) || 0)} 步`;
	const started = new Date(String(data.startedAt || "")).getTime();
	return Number.isFinite(started) ? `工作了 ${Math.max(0, Math.floor((now - started) / 1000))} 秒` : "";
}
function reactions(message: Message): Record<string,string> { try { return message.reaction ? JSON.parse(message.reaction) as Record<string,string> : {}; } catch { return {}; } }
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
  try {
    const value = JSON.parse(assistant.avatar || "") as { shape?: string; color?: string; image?: unknown };
    if (value.shape && value.color) {
      const color = nearestAvatarColor(value.color);
      return { shape: canonicalShape(value.shape), color, colorHex: /^#/.test(value.color) ? avatarColorHex(value.color) : avatarColorHex(color), image: avatarImage(value.image) };
    }
  } catch { /* deterministic fallback */ }
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
  if (["running", "dispatching"].includes(status) || latest.some(message => ["processing", "greeting_processing"].includes(message.kind))) return "working";
  if (status === "failed" || latest[0]?.kind === "error") return "error";
  if (["done", "completed"].includes(status)) return "done";
  return "idle";
}
function cardData(message: Message): Record<string, unknown> {
  try { return message.cardJSON ? JSON.parse(message.cardJSON) as Record<string, unknown> : {}; } catch { return {}; }
}
function messageImages(message: Message): string[] { return arrayData<string>(cardData(message).images); }
function messageSummary(message: Message): string {
	const data = cardData(message);
	const source = String(data.title || data.summary || message.text || (message.kind === "task" ? "任务" : "消息"));
	const firstLine = source.split(/\r?\n/).find(line => line.trim())?.trim() || "消息";
	return Array.from(firstLine).slice(0, 120).join("");
}
function replyAuthor(message: Message): { short: string; possessive: string } {
	return message.role === "user" ? { short: "你", possessive: "你的" } : { short: "助理", possessive: "助理" };
}
function retryPayload(message: Message): { text:string; images:string[] } | undefined {
  const retry = cardData(message).retry;
  if (!retry || typeof retry !== "object") return undefined;
  const value = retry as Record<string, unknown>;
  return { text: String(value.text || ""), images: arrayData<string>(value.images) };
}
function decodedDataURIBytes(dataUri: string): number {
  const payload = dataUri.slice(dataUri.indexOf(",") + 1);
  return Math.max(0, Math.floor(payload.length * 3 / 4) - (payload.endsWith("==") ? 2 : payload.endsWith("=") ? 1 : 0));
}
function avatarForeground(color: string): string {
  const channels = [1, 3, 5].map(index => Number.parseInt(color.slice(index, index + 2), 16) / 255).map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  const luminance = .2126 * channels[0] + .7152 * channels[1] + .0722 * channels[2];
  return (luminance + .05) / .012 > 1.05 / (luminance + .05) ? "#0b0b0c" : "#ffffff";
}
function expiryLabel(value: unknown): string {
  if (!value) return "未说明";
  const milliseconds = new Date(String(value)).getTime() - Date.now();
  if (!Number.isFinite(milliseconds)) return "未说明";
  const minutes = Math.max(0, Math.ceil(milliseconds / 60000));
  return milliseconds <= 0 ? "已过期" : `${minutes} 分钟后过期`;
}
function decisionLabel(data: Record<string, unknown>): string {
  const rejected = String(data.cardState) === "rejected" || String(data.decision) === "reject";
	const always = String(data.decision) === "approve_always";
  const decidedAt = data.decidedAt ? new Date(String(data.decidedAt)).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", timeZone: "Asia/Shanghai" }) : "";
  return `${rejected ? "已拒绝" : always ? "已允许（以后都允许）" : "已允许一次"}${decidedAt ? ` · ${decidedAt}` : ""}`;
}
class PuckConversationErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() {
    if (this.state.failed) return <div className="puck-conversation-error" role="alert"><strong>这段对话暂时无法显示</strong><p>助理列表仍可使用。请切换助理，或稍后重试。</p></div>;
    return this.props.children;
  }
}
const taskLabels: Record<string, string> = { running:"进行中", dispatching:"派发中", waiting_user:"等待回复", awaiting_authorization:"等待授权", stopped:"已停止", reconciling:"正在核对", done:"已完成", failed:"失败" };
function QuestionCard({ message, submitting, onAction }: { message: Message; submitting: boolean; onAction: (kind:string,data:Record<string,unknown>,value:unknown)=>void }) {
  const customAnswerIme = useImeEnterGuard();
  const data = normalizeQuestionData(message);
  const questions = data.questions;
  const cardState = String(data.cardState || "pending");
  const terminal = cardState !== "pending";
  const [selected, setSelected] = useState<Record<string,string[]>>({});
  const [custom, setCustom] = useState<Record<string,string>>({});
  const answers = arrayData<Record<string,unknown>>(data.answers);
  const requiresSubmit = questions.length > 1 || questions.some(question => question.multiSelect || question.allowCustom);
  function choose(question: Question, value: string) {
    if (!requiresSubmit) { onAction("question", data, [{ questionId: question.id, values: [value] } satisfies QuestionAnswer]); return; }
    setSelected(current => {
      const values = current[question.id] || [];
      const next = question.multiSelect ? (values.includes(value) ? values.filter(item => item !== value) : [...values, value]) : [value];
      return { ...current, [question.id]: next };
    });
  }
  const pendingAnswers = questions.map(question => ({ questionId: question.id, values: selected[question.id] || [], ...(custom[question.id]?.trim() ? { custom: custom[question.id].trim() } : {}) }));
  const canSubmit = pendingAnswers.every(answer => answer.values.length > 0 || Boolean(answer.custom));
  if (cardState === "invalid") return <section className="puck-card puck-question-card terminal" role="group" aria-label="问题卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">?</span><strong>问题卡未生成</strong></div><p>{message.text}</p>
  </section>;
  return <section className={`puck-card puck-question-card${terminal ? " terminal" : ""}`} role="group" aria-label="问题卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">?</span><strong>{terminal ? "已回答" : "需要你决定"}</strong></div>
    {questions.length === 0 ? <p>{"助手发来了一个无法识别的问题"}</p> : questions.map((question, questionIndex) => {
      const saved = answers.find(answer => String(answer.questionId) === question.id);
      const savedValues = arrayData<string>(saved?.values);
      return <fieldset className="puck-question" key={question.id}>
        <legend>{questions.length > 1 && <span>{questionIndex + 1}.</span>} {question.question}</legend>
        {!terminal && <div className="puck-card-actions puck-options">{question.options.map(option => {
          const active = (selected[question.id] || []).includes(option.value);
          return <button className={`puck-chip${option.description ? " structured" : ""}${active ? " selected" : ""}`} aria-pressed={requiresSubmit ? active : undefined} key={option.value} type="button" disabled={submitting} onClick={()=>choose(question,option.value)}><span>{option.label}</span>{option.description && <small>{option.description}</small>}</button>;
        })}</div>}
        {!terminal && question.allowCustom && <label className="puck-custom-answer"><span>自定义回答</span><input aria-label={`自定义回答：${question.question}`} value={custom[question.id] || ""} disabled={submitting} onChange={event=>setCustom(current=>({...current,[question.id]:event.target.value}))} onCompositionStart={customAnswerIme.compositionStart} onCompositionEnd={customAnswerIme.compositionEnd} onBlur={customAnswerIme.blur}/></label>}
        {terminal && saved && <p className="puck-card-result"><span aria-hidden="true">✓</span> 你选择了 <strong>{[...savedValues.map(value => question.options.find(option => option.value === value)?.label || value), String(saved.custom || "")].filter(Boolean).join("、")}</strong></p>}
      </fieldset>;
    })}
    {!terminal && questions.length > 0 && requiresSubmit && <div className="puck-card-actions"><button className="puck-btn primary" type="button" disabled={submitting || !canSubmit} onClick={()=>onAction("question",data,pendingAnswers)}>提交回答</button></div>}
    {terminal && answers.length === 0 && data.answer != null && <p className="puck-card-result"><span aria-hidden="true">✓</span> 你选择了 <strong>{String(data.answer)}</strong></p>}
  </section>;
}
function Card({ message, submitting, onAction }: { message: Message; submitting: boolean; onAction: (kind:string,data:Record<string,unknown>,value:unknown)=>void }) {
  const data = cardData(message);
  const cardState = String(data.cardState || "pending");
  const terminal = cardState !== "pending";
  if (message.kind === "question") return <QuestionCard message={message} submitting={submitting} onAction={onAction}/>;
  if (message.kind === "authorization" && cardState === "invalid") return <section className="puck-card puck-authorization-card terminal" role="group" aria-label="授权卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">◇</span><strong>授权卡未生成</strong></div><p>{message.text}</p>
  </section>;
  const expiresAt = data.expiresAt ? new Date(String(data.expiresAt)).getTime() : Number.POSITIVE_INFINITY;
  const expired = message.kind === "authorization" && Number.isFinite(expiresAt) && expiresAt <= Date.now();
  if (message.kind === "authorization" && expired && !terminal) return <section className="puck-card puck-authorization-card terminal compact" role="group" aria-label="授权卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">◇</span><strong>授权已过期</strong><span className="puck-card-result">已过期</span></div>
  </section>;
  if (message.kind === "authorization") return <section className={`puck-card puck-authorization-card${terminal ? " terminal" : ""}`} role="group" aria-label="授权卡片">
    <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">◇</span><strong>{terminal ? "授权已处理" : "需要授权"}</strong></div>
    <p>{message.text}</p>
    <dl>{data.action != null && <><dt>动作</dt><dd>{String(data.action)}</dd></>}{data.deviceId != null && <><dt>主机</dt><dd>{String(data.deviceId)}</dd></>}{data.cwd != null && <><dt>目录</dt><dd className="puck-mono">{String(data.cwd)}</dd></>}{data.expiresAt != null && <><dt>有效期</dt><dd title={new Date(String(data.expiresAt)).toLocaleString("zh-CN")}>{expiryLabel(data.expiresAt)}</dd></>}</dl>
    {(data.risk != null || data.riskTags != null) && <p className="puck-risk"><span aria-hidden="true">△</span>风险：{String(data.risk || arrayData<string>(data.riskTags).join("、") || "无额外风险")}</p>}
    {data.reason != null && <p className="puck-risk"><span aria-hidden="true">△</span>原因：{String(data.reason)}</p>}
    {!terminal ? <div className="puck-card-actions puck-authorization-actions"><button className="puck-btn" type="button" disabled={submitting} onClick={()=>onAction("authorization",data,"reject")}>拒绝</button><button className="puck-btn primary" type="button" disabled={submitting} onClick={()=>onAction("authorization",data,"approve_once")}>仅这一次允许</button><button className="puck-btn primary" type="button" disabled={submitting} onClick={()=>onAction("authorization",data,"approve_always")}>以后都允许</button></div> : <p className="puck-card-result">{decisionLabel(data)}</p>}
  </section>;
  if (message.kind === "share_result") {
    if (cardState === "invalid") return <section className="puck-card puck-result-card terminal" role="group" aria-label="结果卡片">
      <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">!</span><strong>结果卡未生成</strong></div><p>{message.text}</p>
    </section>;
    const items = arrayData<Record<string,unknown>>(data.items).filter(item => item && typeof item === "object");
    return <section className="puck-card puck-result-card" role="group" aria-label="结果卡片">
      <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">✓</span><strong>{String(data.summary || message.text || "助手分享了一项结果")}</strong></div>
      {items.length > 0 ? <ul>{items.map((item,index)=><li key={`${String(item.kind)}-${index}`}><strong>{String(item.title || "未命名结果")}</strong>{item.description != null && <small>{String(item.description)}</small>}</li>)}</ul> : <p>没有更多详情。需要的话可以让助理重新整理。</p>}
    </section>;
  }
  if (message.kind === "task") {
    const status = String(data.status || "pending");
    return <section className="puck-card puck-task-card" role="group" aria-label="任务卡片">
      <div className="puck-card-title"><span className="puck-card-icon" aria-hidden="true">›_</span><strong>{message.text}</strong><span className="puck-state" data-state={status}><i aria-hidden="true"/>{taskLabels[status] || status || "待处理"}</span></div>
      <details><summary><span aria-hidden="true">›</span>{message.workSummary || "查看工作过程"}</summary><p>{String(data.summary || "执行细节按需展开，不在对话中刷屏。")}</p></details>
      {Boolean(data.deviceId && data.threadId) && <div className="puck-card-actions"><button className="puck-btn" type="button" onClick={()=>onAction("task",data,"open")}>打开会话 <span aria-hidden="true">↗</span></button></div>}
    </section>;
  }
  return null;
}
function archivedAuthorization(message: Message, now = Date.now()): boolean {
  if (message.kind !== "authorization") return false;
  const data = cardData(message);
  const cardState = String(data.cardState || "pending");
  if (cardState !== "pending") return true;
  const expiresAt = data.expiresAt ? new Date(String(data.expiresAt)).getTime() : Number.POSITIVE_INFINITY;
  return Number.isFinite(expiresAt) && expiresAt <= now;
}

export function PuckWorkspace({ request, subscribeEvents, state, reason, devices, onBack, onOpenThread }: { request: Request; subscribeEvents?: SubscribeEvents; state: "ready" | "unavailable" | "disabled"; reason?: string; devices: Device[]; onBack?: () => void; onOpenThread?: (deviceId: string, threadId: string) => void }) {
  const createIme = useImeEnterGuard();
  const composerIme = useImeEnterGuard();
  const [assistants, setAssistants] = useState<Assistant[]>([]);
  const [groups, setGroups] = useState<PuckGroup[]>([]);
  const [activeGroup, setActiveGroup] = useState<PuckGroup | null>(null);
  const [groupMessages, setGroupMessages] = useState<Message[]>([]);
  const [creatingGroup, setCreatingGroup] = useState(false);
  const [groupName, setGroupName] = useState("");
  const [groupMemberIDs, setGroupMemberIDs] = useState<string[]>([]);
  const [maxActiveAssistants, setMaxActiveAssistants] = useState<number>();
  const [selectedID, setSelectedID] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [screenshots, setScreenshots] = useState<ScreenshotDraft[]>([]);
  const [notice, setNotice] = useState("");
  const [creating, setCreating] = useState(false);
	const [deleteTarget, setDeleteTarget] = useState<Assistant | undefined>(undefined);
	const [deleting, setDeleting] = useState(false);
  const [name, setName] = useState("");
  const [brainDeviceID, setBrainDeviceID] = useState("");
	const [submittingCard, setSubmittingCard] = useState("__none__");
	const [authorizationRules, setAuthorizationRules] = useState<AuthorizationRule[]>([]);
	const [watchHealthy, setWatchHealthy] = useState(false);
	const [mobilePanel, setMobilePanel] = useState<"" | "assistants" | "info">("");
	const avatarUpdateSequence = useRef(0);
  const listSequence = useRef(0);
  const screenshotInputRef = useRef<HTMLInputElement>(null);
  const conversationRef = useRef<HTMLElement>(null);
  const transcriptRef = useRef<HTMLDivElement>(null);
  const composerFooterRef = useRef<HTMLElement>(null);
  const selectedRef = useRef("");
  const messageAssistantRef = useRef("");
	const lastAssistantRef = useRef(storedLastAssistant());
	const assistantHistoryRef = useRef<string[]>(lastAssistantRef.current ? [lastAssistantRef.current] : []);
  const followLatestRef = useRef(true);
  const [clock, setClock] = useState(() => Date.now());
  const [reactionTarget, setReactionTarget] = useState("");
  const reactionLongPressTimer = useRef<number | undefined>(undefined);
  const [replyTarget, setReplyTarget] = useState<Message | undefined>(undefined);
  const messagePointer = useRef<{ id: string; x: number; y: number } | undefined>(undefined);
  const highlightTimer = useRef<number | undefined>(undefined);
  const [editingDescription, setEditingDescription] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState("");
  const faviconOriginal = useRef<{ href:string; type:string } | undefined>(undefined);

  useEffect(() => {
    const conversation = conversationRef.current;
    const footer = composerFooterRef.current;
    if (!conversation || !footer || typeof ResizeObserver === "undefined") return;
    const updateHeight = () => {
      conversation.style.setProperty("--puck-footer-overlay-height", `${footer.getBoundingClientRect().height}px`);
      if (followLatestRef.current) scrollTranscriptToLatest();
    };
    const observer = new ResizeObserver(updateHeight);
    observer.observe(footer);
    updateHeight();
    return () => {
      observer.disconnect();
      conversation.style.removeProperty("--puck-footer-overlay-height");
    };
  }, [selectedID]);
  const readyBrains = useMemo(() => devices.filter(device => device.capabilities?.puckBrainReady), [devices]);
  const selected = assistants.find(assistant => assistant.assistantId === selectedID);
  const puckState = messageState(messages);
  const busy = messages.some(message => ["processing", "greeting_processing"].includes(message.kind) || (message.kind === "task" && ["running", "dispatching", "waiting_user", "awaiting_authorization"].includes(String(cardData(message).status))));
  selectedRef.current = selectedID;

  function selectAssistant(assistantID: string, initialMessages: Message[] = []) {
    if (assistantID === selectedRef.current && messageAssistantRef.current === assistantID && initialMessages.length === 0) return;
    cancelReactionLongPress();
    followLatestRef.current = true;
    selectedRef.current = assistantID;
    messageAssistantRef.current = assistantID;
	if (assistantID) {
		lastAssistantRef.current = assistantID;
		assistantHistoryRef.current = [assistantID, ...assistantHistoryRef.current.filter(id => id !== assistantID)];
		storeLastAssistant(assistantID);
	}
    setMessages(initialMessages);
    setReactionTarget("");
    setReplyTarget(undefined);
    setActiveGroup(null);
    setSelectedID(assistantID);
  }
  function selectGroup(group: PuckGroup) {
    cancelReactionLongPress();
    followLatestRef.current = true;
    setActiveGroup(group);
    setGroupMessages([]);
    setReplyTarget(undefined);
    setMobilePanel("");
  }
  function updateScopedMessages(assistantID: string, update: (current: Message[]) => Message[]) {
    if (selectedRef.current !== assistantID || messageAssistantRef.current !== assistantID) return;
    setMessages(current => selectedRef.current === assistantID && messageAssistantRef.current === assistantID ? update(current) : current);
  }
  function scrollTranscriptToLatest() {
    const transcript = transcriptRef.current;
    if (transcript) transcript.scrollTop = Math.max(0, transcript.scrollHeight - transcript.clientHeight);
  }
  function trackTranscriptScroll() {
    const transcript = transcriptRef.current;
    if (!transcript) return;
    const distance = Math.max(0, transcript.scrollHeight - transcript.clientHeight - transcript.scrollTop);
    followLatestRef.current = distance <= latestScrollThreshold;
  }

  useLayoutEffect(() => {
    if (followLatestRef.current) scrollTranscriptToLatest();
  }, [selectedID, messages]);

  useEffect(() => {
	const running = messages.some(message => workData(message).state === "running");
	const pendingAuthorization = messages.some(message => message.kind === "authorization" && String(cardData(message).cardState || "pending") === "pending");
	if (!running && !pendingAuthorization) return;
	setClock(Date.now()); const timer = window.setInterval(() => setClock(Date.now()), running ? 1000 : 5000);
	return () => window.clearInterval(timer);
  }, [messages]);

  async function loadList() {
    const sequence = ++listSequence.current;
    const response = await request("puck.list", "relay", {});
    if (sequence !== listSequence.current) return;
    if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
    const groupResponse = await request("puck.group.list", "relay", {});
    if (sequence === listSequence.current && groupResponse.outcome === "accepted") {
      const nextGroups = arrayData<PuckGroup>(groupResponse.data?.groups);
      setGroups(nextGroups);
      setActiveGroup(current => current ? nextGroups.find(group => group.groupId === current.groupId) || null : current);
    }
    const next = arrayData<Assistant>(response.data?.assistants);
    const reportedLimit = Number(response.data?.maxActiveAssistants);
    if (Number.isInteger(reportedLimit) && reportedLimit > 0) setMaxActiveAssistants(reportedLimit);
    const currentID = selectedRef.current;
    const preferredID = lastAssistantRef.current;
    const nextID = currentID && next.some(item => item.assistantId === currentID)
      ? currentID
      : preferredID && next.some(item => item.assistantId === preferredID) ? preferredID : next[0]?.assistantId || "";
    setAssistants(next.map(item => item.assistantId === nextID ? { ...item, unreadCount: 0 } : item));
    if (nextID !== currentID) selectAssistant(nextID);
  }
  useEffect(() => { if (state !== "ready") return; void loadList(); const timer = window.setInterval(() => void loadList(), watchHealthy ? 60000 : 5000); return () => window.clearInterval(timer); }, [state, watchHealthy]);
  useEffect(() => {
	if (!selectedID) { messageAssistantRef.current = ""; setMessages([]); setAuthorizationRules([]); return; }
	let active = true;
	if (messageAssistantRef.current !== selectedID) { messageAssistantRef.current = selectedID; setMessages([]); }
	setEditingDescription(false); setDescriptionDraft(assistants.find(item => item.assistantId === selectedID)?.description || "");
	setAssistants(current => current.map(item => item.assistantId === selectedID ? { ...item, unreadCount: 0 } : item));
	void request("puck.read", "relay", { assistantId: selectedID });
	setAuthorizationRules([]);
	void request("puck.authorization.rule.list", "relay", { assistantId: selectedID }).then(response => {
		if (active && response.outcome === "accepted") setAuthorizationRules(arrayData<AuthorizationRule>(response.data?.rules));
	});
    void request("puck.history", "relay", { assistantId: selectedID }).then(response => {
      if (active && response.outcome === "accepted") {
        const durable = historyMessages(response.data?.messages, selectedID);
        updateScopedMessages(selectedID, current => mergeHistory(current, durable));
      }
    });
    return () => { active = false; };
  }, [selectedID]);
	useEffect(() => {
		if (!selectedID) { setWatchHealthy(false); return; }
		let active = true;
		let watchID = "";
		setWatchHealthy(false);
		const unsubscribe = subscribeEvents?.(event => {
			if (!active || event.event !== "puck.event" || event.assistantId !== selectedID || event.kind !== "history") return;
			const payload = event.payload as Record<string, unknown> | undefined;
			const durable = historyMessages(payload?.messages, selectedID);
			updateScopedMessages(selectedID, current => mergeHistory(current, durable));
			void request("puck.read", "relay", { assistantId: selectedID });
		});
		if (subscribeEvents) {
			void request("puck.watch", "relay", { assistantId: selectedID }).then(response => {
				const id = response.outcome === "accepted" ? String(response.data?.watchId || "") : "";
				if (!id) { if (active) setWatchHealthy(false); return; }
				if (!active) void request("puck.unwatch", "relay", { watchId: id });
				else { watchID = id; setWatchHealthy(true); }
			}).catch(() => { if (active) setWatchHealthy(false); });
		}
		return () => {
			active = false;
			unsubscribe?.();
			if (watchID) void request("puck.unwatch", "relay", { watchId: watchID });
		};
	}, [selectedID, subscribeEvents]);
	useEffect(() => {
		if (!selectedID) return;
		let active = true;
		const timer = window.setInterval(() => { void request("puck.history", "relay", { assistantId: selectedID }).then(response => {
			if (!active || response.outcome !== "accepted") return;
			const durable = historyMessages(response.data?.messages, selectedID);
			updateScopedMessages(selectedID, current => mergeHistory(current, durable));
		}); }, watchHealthy ? 60000 : subscribeEvents ? 10000 : 2000);
		return () => { active = false; window.clearInterval(timer); };
	}, [selectedID, subscribeEvents, watchHealthy]);

  useEffect(() => {
    if (!activeGroup) return;
    let active = true;
    const groupID = activeGroup.groupId;
    const pull = () => request("puck.group.history", "relay", { groupId: groupID }).then(response => {
      if (active && response.outcome === "accepted") setGroupMessages(arrayData<Record<string, unknown>>(response.data?.messages).map(normalizeMessage));
    });
    void pull();
    const timer = window.setInterval(() => void pull(), 2000);
    return () => { active = false; window.clearInterval(timer); };
  }, [activeGroup?.groupId]);
  async function createGroup() {
    const name = groupName.trim();
    if (!name || groupMemberIDs.length < 2) { setNotice("请填写组名，并至少选择两个助理。"); return; }
    const response = await request("puck.group.create", "relay", { name, memberIds: groupMemberIDs });
    if (response.outcome !== "accepted" || !response.data?.group) { setNotice(responseError(response)); return; }
    const group = response.data.group as PuckGroup;
    setGroups(current => [...current.filter(item => item.groupId !== group.groupId), group]);
    setCreatingGroup(false);
    setGroupName("");
    setGroupMemberIDs([]);
    selectGroup(group);
  }
  async function sendGroup() {
    if (!activeGroup) return;
    const text = draft.trim();
    if (!text) return;
    const groupID = activeGroup.groupId;
    const clientMessageId = crypto.randomUUID();
    followLatestRef.current = true;
    setDraft("");
    setGroupMessages(current => [...current, { id: clientMessageId, role: "user", kind: "text", text }, ...activeGroup.members.map(member => ({ id: `${clientMessageId}-${member.assistantId}`, role: "assistant", kind: "processing", text: "正在处理…", authorAssistantId: member.assistantId, authorName: member.name }))]);
    const response = await request("puck.group.message.send", "relay", { groupId: groupID, clientMessageId, text });
    if (response.outcome !== "accepted") {
      setGroupMessages(current => current.map(item => item.id.startsWith(`${clientMessageId}-`) ? { ...item, kind: "error", text: responseError(response) } : item));
      return;
    }
    setGroupMessages(arrayData<Record<string, unknown>>(response.data?.messages).map(normalizeMessage));
  }
  async function createAssistant() {
    if (maxActiveAssistants !== undefined && assistants.length >= maxActiveAssistants) { setNotice(`最多可创建 ${maxActiveAssistants} 个 Bot。`); return; }
    const deviceID = brainDeviceID || readyBrains[0]?.deviceId;
    if (!name.trim() || !deviceID) { setNotice("请填写名称并选择可用的大脑主机。"); return; }
    const response = await request("puck.create", "relay", { name: name.trim(), brainDeviceId: deviceID, allowedScope: {}, permissionMode: "full" });
    if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
    listSequence.current += 1;
    const assistant = response.data?.assistant as Assistant;
    const greeting = response.data?.greeting as Record<string,unknown> | undefined;
	const greetingMessages: Message[] = greeting?.state === "processing" ? [{ id:String(greeting.messageId), assistantId:assistant.assistantId, role:"assistant", kind:"processing", text:"正在准备…", workSummary:JSON.stringify({startedAt:greeting.startedAt,state:"running"}) }] : [];
    setAssistants(current => [...current, assistant]); selectAssistant(assistant.assistantId, greetingMessages); setName(""); setCreating(false);
  }
  async function send() {
    if (activeGroup) { await sendGroup(); return; }
    const text = draft.trim(); if (!selected || (!text && screenshots.length === 0)) return;
    if (screenshots.length === 0 && ["停", "停止"].includes(text)) { setDraft(""); await stop(); return; }
    const images = screenshots.map(image => image.dataUri);
    const assistantID = selected.assistantId;
    followLatestRef.current = true;
    lastAssistantRef.current = assistantID;
    storeLastAssistant(assistantID);
    const replyTo = replyTarget?.id;
    const clientMessageId = crypto.randomUUID(); setDraft(""); setScreenshots([]); setReplyTarget(undefined); if (screenshotInputRef.current) screenshotInputRef.current.value = ""; const localStartedAt = new Date().toISOString();
    updateScopedMessages(assistantID, current => [...current, { id: clientMessageId, assistantId:assistantID, role: "user", kind: "text", text, cardJSON: images.length ? JSON.stringify({ images }) : "", replyTo }, { id: `${clientMessageId}-processing`, assistantId:assistantID, role: "assistant", kind: "processing", text: "正在处理…", workSummary:JSON.stringify({startedAt:localStartedAt,state:"running"}) }]);
    const params: Record<string, unknown> = { assistantId: assistantID, clientMessageId, text, images };
    if (replyTo) params.replyTo = replyTo;
    const response = await request("puck.message.send", "relay", params);
    if (response.outcome !== "accepted") updateScopedMessages(assistantID, current => current.map(item => item.id === `${clientMessageId}-processing` ? { ...item, kind: "error", text: responseError(response), cardJSON:JSON.stringify({retry:{text,images}}), workSummary:"" } : item));
	else updateScopedMessages(assistantID, current => current.map(item => item.id === `${clientMessageId}-processing` ? { ...item, workSummary:JSON.stringify({startedAt:response.data?.startedAt || localStartedAt,state:"running"}) } : item));
  }
  function restoreRetry(message: Message) {
    const retry = retryPayload(message); if (!retry) return;
    setDraft(retry.text);
    setScreenshots(retry.images.map((dataUri,index) => ({ name:`重试图片 ${index + 1}.${dataUri.startsWith("data:image/jpeg") ? "jpg" : "png"}`, bytes:decodedDataURIBytes(dataUri), dataUri })));
    setNotice("内容已放回输入框，请再次发送。");
  }
  function beginReactionLongPress(messageID: string, pointerType: string) {
    if (pointerType !== "touch") return;
    if (reactionLongPressTimer.current !== undefined) window.clearTimeout(reactionLongPressTimer.current);
    reactionLongPressTimer.current = window.setTimeout(() => { setReactionTarget(messageID); reactionLongPressTimer.current = undefined; }, 500);
  }
  function cancelReactionLongPress() {
    if (reactionLongPressTimer.current !== undefined) window.clearTimeout(reactionLongPressTimer.current);
    reactionLongPressTimer.current = undefined;
  }
  function beginMessagePointer(message: Message, pointerType: string, x: number, y: number) {
	messagePointer.current = pointerType === "touch" ? { id: message.id, x, y } : undefined;
	beginReactionLongPress(message.id, pointerType);
  }
  function endMessagePointer(message: Message, pointerType: string, x: number, y: number) {
	const start = messagePointer.current;
	messagePointer.current = undefined;
	cancelReactionLongPress();
	if (pointerType === "touch" && start?.id === message.id && start.x - x >= 56 && Math.abs(start.y - y) <= 44) setReplyTarget(message);
  }
  async function jumpToReply(messageID: string) {
	if (!selected) return;
	let target = messages.find(message => message.id === messageID);
	if (!target) {
		const assistantID = selected.assistantId;
		const response = await request("puck.message.get", "relay", { assistantId: assistantID, messageId: messageID });
		if (response.outcome !== "accepted" || !response.data?.message) { setNotice("原消息暂不可用"); return; }
		target = normalizeMessage(response.data.message as Record<string, unknown>);
		updateScopedMessages(assistantID, current => current.some(message => message.id === target!.id) ? current : [target!, ...current]);
		await new Promise<void>(resolve => window.requestAnimationFrame(() => resolve()));
	}
	const element = transcriptRef.current?.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(messageID)}"]`);
	if (!element) { setNotice("原消息暂不可用"); return; }
	element.scrollIntoView({ behavior: "smooth", block: "center" });
	element.classList.add("reply-highlight");
	if (highlightTimer.current !== undefined) window.clearTimeout(highlightTimer.current);
	highlightTimer.current = window.setTimeout(() => { element.classList.remove("reply-highlight"); highlightTimer.current = undefined; }, 1500);
  }
  async function addScreenshots(files: FileList | null) {
    if (!files?.length) return;
    try { setScreenshots(await readScreenshotFiles(screenshots, files)); setNotice(""); }
    catch (error) { setNotice(error instanceof Error ? error.message : "无法读取截图。"); }
    finally { if (screenshotInputRef.current) screenshotInputRef.current.value = ""; }
  }
  async function stop() {
    if (!selected) return;
    const assistantID = selected.assistantId;
    const response = await request("puck.stop", "relay", { assistantId: assistantID });
    if (response.outcome === "accepted" && response.data?.message) updateScopedMessages(assistantID, current => [...current.filter(item => item.kind !== "processing"), normalizeMessage(response.data!.message as Record<string, unknown>)]);
    else if (response.outcome !== "accepted") updateScopedMessages(assistantID, current => [...current, { id: crypto.randomUUID(), assistantId:assistantID, role: "assistant", kind: "error", text: responseError(response) }]);
  }
	async function respondCard(kind:string,data:Record<string,unknown>,value:unknown) {
		if (!selected) return;
		if (kind === "task") {
			onOpenThread?.(String(data.deviceId || ""), String(data.threadId || ""));
			return;
		}
		const identity = String(data.authorizationId || data.questionId || "");
		const assistantID = selected.assistantId;
		if (submittingCard === identity) return;
		setSubmittingCard(identity);
		const response = kind === "authorization"
			? await request("puck.authorization.decide","relay",{assistantId:assistantID,authorizationId:String(data.authorizationId||""),decision:String(value)})
			: await request("puck.question.answer","relay",{assistantId:assistantID,questionId:String(data.questionId||""),answers:value});
		if (response.outcome === "accepted") {
			updateScopedMessages(assistantID, current => current.map(message => {
			const card = kind === "question" ? normalizeQuestionData(message) : cardData(message);
			if (String(card.authorizationId || card.questionId || "") !== identity) return message;
			return {...message, cardJSON: JSON.stringify({...card, cardState: kind === "question" ? "answered" : value === "reject" ? "rejected" : "consumed", ...(kind === "question" ? {answers:value} : {decision:value})})};
			}));
			if (kind === "authorization" && value === "approve_always") void request("puck.authorization.rule.list", "relay", { assistantId: assistantID }).then(listed => {
				if (selectedRef.current === assistantID && listed.outcome === "accepted") setAuthorizationRules(arrayData<AuthorizationRule>(listed.data?.rules));
			});
		} else setNotice(responseError(response));
		setSubmittingCard("__none__");
	}
	async function updateAvatar(shape: AvatarShape, color: AvatarColor) {
		if (!selected) return;
		const currentDesign = avatarDesign(selected);
		if (currentDesign.shape === shape && currentDesign.color === color) return;
		const previousAvatar = selected.avatar;
		const nextAvatar = { shape, color, ...(currentDesign.image ? { image: currentDesign.image } : {}) };
		const optimistic = { ...selected, avatar: JSON.stringify(nextAvatar) };
		const sequence = ++avatarUpdateSequence.current;
		setAssistants(current => current.map(item => item.assistantId === selected.assistantId ? optimistic : item));
		const response = await request("puck.update", "relay", { assistantId: selected.assistantId, avatar: nextAvatar });
		if (sequence !== avatarUpdateSequence.current) return;
		if (response.outcome !== "accepted") {
			setAssistants(current => current.map(item => item.assistantId === selected.assistantId ? { ...item, avatar: previousAvatar } : item));
			setNotice(responseError(response));
			return;
		}
		const confirmed = response.data?.assistant as Assistant | undefined;
		if (confirmed) setAssistants(current => current.map(item => item.assistantId === confirmed.assistantId ? confirmed : item));
	}
	async function saveDescription() {
		if (!selected) return; const description = descriptionDraft.trim();
		const response = await request("puck.update", "relay", { assistantId:selected.assistantId, description });
		if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
		const confirmed = response.data?.assistant as Assistant | undefined;
		setAssistants(current => current.map(item => item.assistantId === selected.assistantId ? (confirmed || { ...item, description }) : item)); setEditingDescription(false);
	}
	async function updatePermissionMode(permissionMode: "full" | "ask") {
		if (!selected || (selected.permissionMode || "full") === permissionMode) return;
		const response = await request("puck.update", "relay", { assistantId: selected.assistantId, permissionMode });
		if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
		const confirmed = response.data?.assistant as Assistant | undefined;
		setAssistants(current => current.map(item => item.assistantId === selected.assistantId ? (confirmed || { ...item, permissionMode }) : item));
	}
	async function revokeAuthorizationRule(ruleID: string) {
		if (!selected) return;
		const assistantID = selected.assistantId;
		const response = await request("puck.authorization.rule.revoke", "relay", { assistantId: assistantID, ruleId: ruleID });
		if (response.outcome === "accepted") setAuthorizationRules(current => current.filter(rule => rule.id !== ruleID));
		else setNotice(responseError(response));
	}
	async function deleteAssistant() {
		if (!deleteTarget || deleting) return;
		setDeleting(true);
		const target = deleteTarget;
		const response = await request("puck.delete", "relay", { assistantId: target.assistantId });
		if (response.outcome !== "accepted") {
			setNotice(responseError(response));
			setDeleting(false);
			return;
		}
		listSequence.current += 1;
		const survivors = assistants.filter(item => item.assistantId !== target.assistantId);
		assistantHistoryRef.current = assistantHistoryRef.current.filter(id => id !== target.assistantId);
		const nextID = assistantHistoryRef.current.find(id => survivors.some(item => item.assistantId === id)) || survivors[0]?.assistantId || "";
		setAssistants(survivors);
		setDeleteTarget(undefined);
		setDeleting(false);
		setMobilePanel("");
		if (selectedRef.current === target.assistantId) {
			if (nextID) selectAssistant(nextID);
			else {
				lastAssistantRef.current = "";
				storeLastAssistant("");
				selectAssistant("");
			}
		}
	}
	async function reactTo(message: Message, emoji: string) {
		if (!selected) return; const assistantID = selected.assistantId; const response = await request("puck.message.react", "relay", { assistantId:assistantID, messageId:message.id, emoji });
		if (response.outcome !== "accepted") { setNotice(responseError(response)); return; }
		updateScopedMessages(assistantID, current => current.map(item => item.id === message.id ? { ...item, reaction:String(response.data?.reaction || "") } : item)); if (selectedRef.current === assistantID) setReactionTarget("");
	}

	useEffect(() => {
		const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]'); if (!link) return;
		if (!faviconOriginal.current) faviconOriginal.current = { href:link.getAttribute("href") || "", type:link.getAttribute("type") || "" };
		return () => { const original=faviconOriginal.current; if (original) { link.href=original.href; link.type=original.type; } };
	}, []);
	useEffect(() => () => { cancelReactionLongPress(); if (highlightTimer.current !== undefined) window.clearTimeout(highlightTimer.current); }, []);
	useEffect(() => {
		if (!selected || navigator.userAgent.includes("jsdom")) return;
		let cancelled=false; const svg=document.querySelector<SVGSVGElement>(".puck-conversation > header .puck-avatar svg"); if (!svg) return;
		const clone=svg.cloneNode(true) as SVGSVGElement; clone.setAttribute("xmlns","http://www.w3.org/2000/svg"); clone.setAttribute("width","128"); clone.setAttribute("height","128");
		clone.querySelectorAll<SVGElement>(".puck-avatar-body,.puck-avatar-ears,.puck-avatar-stem").forEach(node=>node.setAttribute("fill",avatarDesign(selected).color)); clone.querySelectorAll<SVGElement>(".puck-avatar-face").forEach(node=>node.setAttribute("fill","#0b0b0c"));
		const image=new Image(); image.onload=()=>{ if(cancelled)return; const canvas=document.createElement("canvas"); canvas.width=canvas.height=128; const context=canvas.getContext("2d"); if(!context)return; context.drawImage(image,0,0,128,128); const link=document.querySelector<HTMLLinkElement>('link[rel="icon"]'); if(link){link.type="image/png";link.href=canvas.toDataURL("image/png");} };
		image.src=`data:image/svg+xml;charset=utf-8,${encodeURIComponent(new XMLSerializer().serializeToString(clone))}`; return()=>{cancelled=true;};
	}, [selectedID, selected?.avatar]);

  if (state !== "ready") return <main className="puck-unavailable">
    <button className="puck-btn" type="button" onClick={onBack}>返回会话</button>
    <div><span className="puck-mark">P</span><h1>Puck 不可用</h1><p>{reason === "PUCK_REQUIRES_PASSKEY_HTTPS" ? "Puck 仅支持 Passkey + HTTPS" : "管理员尚未开启 Puck。"}</p><small>现有 Codex 会话不受影响。</small></div>
  </main>;

  const statusLabel = puckState === "waiting_user" ? "等你回复" : puckState === "awaiting_authorization" ? "等待授权" : puckState === "working" || puckState === "thinking" ? "思考中" : puckState === "error" ? "出错" : "可用";
  return <main className="puck-workspace">
    {mobilePanel && <button className="puck-panel-backdrop" aria-label="关闭面板" onClick={()=>setMobilePanel("")}/>}
    <aside className={`puck-assistants ${mobilePanel === "assistants" ? "mobile-open" : ""}`}>
      <header><b>Puck</b><span className="puck-assistant-capacity" aria-label={maxActiveAssistants === undefined ? `助理数量 ${assistants.length}` : `助理数量 ${assistants.length}/${maxActiveAssistants}`}>{assistants.length}{maxActiveAssistants === undefined ? "" : ` / ${maxActiveAssistants}`}</span><button className="puck-group-create" type="button" aria-label="建组聊天" disabled={assistants.length < 2} onClick={() => { setGroupName(""); setGroupMemberIDs([]); setCreatingGroup(true); }}>建组</button><button className="puck-icon-btn puck-add" type="button" onClick={() => setCreating(true)} aria-label="新建助理" disabled={maxActiveAssistants !== undefined && assistants.length >= maxActiveAssistants}>＋</button></header>
      <div className="puck-assistant-list">{groups.map(group => <button className={`puck-assistant-row${activeGroup?.groupId === group.groupId ? " active" : ""}`} key={group.groupId} onClick={() => selectGroup(group)}><span className="puck-group-mark" aria-hidden="true">组</span><span className="puck-assistant-copy"><strong>{group.name}</strong><small>{group.members.map(member => member.name).join("、")}</small></span></button>)}{assistants.map(item => <button className={`puck-assistant-row${!activeGroup && item.assistantId === selectedID ? " active" : ""}`} key={item.assistantId} onClick={() => { selectAssistant(item.assistantId); setMobilePanel(""); }}>
        <PuckAvatar assistantId={item.assistantId} name={item.name} design={avatarDesign(item)} state={item.assistantId === selectedID ? puckState : "idle"} size="small" attention={Boolean(item.unreadCount) || item.assistantId === selectedID && ["waiting_user","awaiting_authorization","error"].includes(puckState)}/>
        <span className="puck-assistant-copy"><strong>{item.name}</strong><small>{item.assistantId === selectedID && messages.at(-1)?.text || item.lastMessagePreview || (item.status === "ready" ? "在线" : "准备中")}</small></span>
        {Boolean(item.unreadCount) && <span className="puck-unread-count" aria-label={`${item.name}有 ${item.unreadCount} 条未读消息`}>{Math.min(99,item.unreadCount || 0)}</span>}
      </button>)}</div>
      <button className="puck-back" type="button" onClick={onBack}><span aria-hidden="true">←</span> Codex 会话</button>
    </aside>

    <section className="puck-conversation" ref={conversationRef}>
      <header>
        <button className="puck-mobile-menu puck-icon-btn puck-glass-chip" type="button" aria-label="打开助理列表" onClick={()=>setMobilePanel("assistants")}>☰</button>
        <button className="puck-header-title puck-glass-chip" type="button" aria-label="查看助理信息" disabled={!selected} onClick={event=>{ if ((event.target as HTMLElement).closest(".puck-avatar")) return; setMobilePanel("info"); }}>
          {activeGroup ? <span className="puck-group-mark" aria-hidden="true">组</span> : selected && <PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState}/>}
          <div className="puck-conversation-title"><strong>{activeGroup?.name || selected?.name || "Puck"}</strong></div>
        </button>
        {selected && <div className="puck-header-actions puck-glass-chip">{busy ? <button className="puck-stop puck-icon-btn" type="button" aria-label="停止全部工作" onClick={() => void stop()}><span aria-hidden="true"/></button> : <button className="puck-icon-btn" type="button" aria-label="更多助理操作" onClick={()=>setMobilePanel("info")}><span className="puck-more" aria-hidden="true">•••</span></button>}</div>}
      </header>
	      <div className="puck-transcript" ref={transcriptRef} aria-live="polite" onScroll={trackTranscriptScroll}>{assistants.length === 0 && groups.length === 0 ? <div className="puck-empty">
        <PuckAvatar assistantId="puck-empty" name="Puck" size="large" animated={false}/><h2>创建你的第一个助理</h2><p>给它一个名字，选择在哪台设备上思考。</p><button className="puck-btn primary puck-btn-md" type="button" aria-label="新建助理" onClick={() => setCreating(true)} disabled={maxActiveAssistants !== undefined && assistants.length >= maxActiveAssistants}>新建助理</button>
	      </div> : <PuckConversationErrorBoundary key={activeGroup?.groupId || selectedID}>{(activeGroup ? groupMessages : messages).map((message,index,transcriptMessages) => {
        const now = clock || Date.now();
        if (archivedAuthorization(message, now)) {
          let start = index;
          while (start > 0 && archivedAuthorization(transcriptMessages[start - 1], now)) start -= 1;
          let count = 0;
          while (start + count < transcriptMessages.length && archivedAuthorization(transcriptMessages[start + count], now)) count += 1;
          if (count >= 2) {
            if (index !== start) return null;
            return <details className="puck-auth-archive" key={`archive-${message.id}`}><summary>已收起 {count} 条已处理或过期的授权</summary>{transcriptMessages.slice(start, start + count).map(item => <Card key={item.id} message={item} submitting={false} onAction={() => undefined}/>)}</details>;
          }
        }
        const continued = index > 0 && transcriptMessages[index - 1].role === message.role;
        const groupEnd = index === transcriptMessages.length - 1 || transcriptMessages[index + 1].role !== message.role;
	        const working = message.role === "assistant" && ["processing", "greeting_processing", "acknowledged"].includes(message.kind);
	        const reactionEntries = Object.entries(reactions(message));
	        const quoted = message.replyTo ? transcriptMessages.find(candidate => candidate.id === message.replyTo) : undefined;
	        const speaker = activeGroup ? assistants.find(item => item.assistantId === message.authorAssistantId) : selected;
	        return <article key={message.id} data-message-id={message.id} className={`puck-message ${message.role} ${message.kind}${continued ? " continued" : ""}${working ? " with-avatar" : ""}`} onPointerDown={event=>beginMessagePointer(message,event.pointerType,event.clientX,event.clientY)} onPointerUp={event=>endMessagePointer(message,event.pointerType,event.clientX,event.clientY)} onPointerCancel={()=>{messagePointer.current=undefined;cancelReactionLongPress();}}>
          {working && speaker && <PuckAvatar assistantId={speaker.assistantId} name={speaker.name} design={avatarDesign(speaker)} state={puckState} size="small"/>}
          <div className="puck-message-content">
	            {activeGroup && message.role === "assistant" && <strong className="puck-group-author">{message.authorName || speaker?.name || "助理"}</strong>}
	            {message.replyTo && <button className="puck-reply-quote" type="button" aria-label={quoted ? "跳到被引用的消息" : "加载并跳到被引用的消息"} onClick={()=>void jumpToReply(message.replyTo!)}><strong>{quoted ? replyAuthor(quoted).short : "引用消息"}</strong><span>{quoted ? messageSummary(quoted) : "点按加载原消息"}</span></button>}
	            <Card message={message} submitting={submittingCard === String(cardData(message).authorizationId || cardData(message).questionId || "")} onAction={(kind,data,value)=>void respondCard(kind,data,value)}/>
	            {!["question", "authorization", "share_result", "task"].includes(message.kind) && <div className="puck-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{message.text}</ReactMarkdown>{["processing", "greeting_processing", "acknowledged"].includes(message.kind) && <span className="puck-thinking-dots" aria-hidden="true"><i/><i/><i/></span>}</div>}
	            {message.kind === "error" && retryPayload(message) && <button className="puck-error-retry" type="button" aria-label="重试发送" onClick={()=>restoreRetry(message)}>重试</button>}
            {messageImages(message).length > 0 && <div className="puck-message-images" aria-label="已发送图片">{messageImages(message).map((image, imageIndex) => <a href={image} target="_blank" rel="noreferrer" key={imageIndex} aria-label={`放大已发送图片 ${imageIndex + 1}`}><img className="puck-message-image" src={image} alt={`已发送图片 ${imageIndex + 1}`}/></a>)}</div>}
            {workLabel(message,clock) && <div className="puck-work-summary">{workLabel(message,clock)}</div>}
	            <div className={`puck-reactions${reactionEntries.length ? " has-reactions" : ""}${reactionTarget === message.id ? " picker-open" : ""}`}>{reactionEntries.map(([actor,emoji]) => <button type="button" className="puck-reaction-pill" aria-label={`${actor === "user" ? "你" : "助理"}回应 ${emoji}，1 个`} key={actor} onClick={()=>actor === "user" && void reactTo(message,emoji)}>{emoji} <span>1</span></button>)}<button type="button" className="puck-reaction-add" aria-label="给消息添加回应" onClick={()=>setReactionTarget(current=>current === message.id ? "" : message.id)}>＋</button>{reactionTarget === message.id && <div className="puck-reaction-picker">{reactionChoices.map(emoji=><button type="button" key={emoji} aria-label={`回应 ${emoji}`} onClick={()=>void reactTo(message,emoji)}>{emoji}</button>)}</div>}</div>
            {groupEnd && message.createdAt && <time>{new Date(message.createdAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", timeZone: "Asia/Shanghai" })}</time>}
          </div>
	          <button className="puck-reply-action" type="button" aria-label={`回复${replyAuthor(message).possessive}消息：${messageSummary(message)}`} onClick={()=>setReplyTarget(message)}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M8.5 6 4.5 10l4 4M5 10h5.5c3 0 5 1.5 5 4.5"/></svg></button>
        </article>;
      })}</PuckConversationErrorBoundary>}</div>
      {notice && <div className="puck-notice" role="alert">{notice}<button className="puck-btn" onClick={() => setNotice("")}>关闭</button></div>}
      {(selected || activeGroup) && <footer ref={composerFooterRef}><div className="puck-composer-shell">{!activeGroup && replyTarget && <div className="puck-reply-preview" aria-label={`正在回复${replyAuthor(replyTarget).possessive}消息`}><span aria-hidden="true" className="puck-reply-preview-mark"/><div><strong>{replyAuthor(replyTarget).short}</strong><span>{messageSummary(replyTarget)}</span></div><button type="button" aria-label="取消回复" onClick={()=>setReplyTarget(undefined)}>×</button></div>}{!activeGroup && screenshots.length > 0 && <div className="puck-screenshot-drafts" aria-label="待发送截图">{screenshots.map((image,index)=><div className="puck-screenshot-draft" key={`${image.name}:${index}`}><img src={image.dataUri} alt={image.name}/><button type="button" aria-label={`移除截图：${image.name}`} onClick={()=>setScreenshots(current=>current.filter((_,position)=>position!==index))}>×</button></div>)}</div>}<div className="puck-composer" style={{"--puck-avatar-color":avatarDesign(selected || assistants[0] || { assistantId: "group", name: activeGroup?.name || "组", brainDeviceId: "", status: "ready" }).colorHex,"--puck-send-foreground":avatarForeground(avatarDesign(selected || assistants[0] || { assistantId: "group", name: activeGroup?.name || "组", brainDeviceId: "", status: "ready" }).colorHex)} as CSSProperties}><input ref={screenshotInputRef} className="puck-screenshot-file" type="file" aria-label="附加截图" accept="image/png,image/jpeg" multiple onChange={event=>void addScreenshots(event.target.files)}/><button className="puck-attach puck-icon-btn" type="button" aria-label="选择截图" onClick={()=>screenshotInputRef.current?.click()} disabled={!!activeGroup}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M10 4v12M4 10h12"/></svg></button><textarea rows={1} aria-label="给 Puck 发消息" value={draft} onChange={event => setDraft(event.target.value)} onCompositionStart={composerIme.compositionStart} onCompositionEnd={composerIme.compositionEnd} onBlur={composerIme.blur} onKeyDown={event => { if (composerIme.classify(event) === "submit") { event.preventDefault(); void send(); } }} placeholder={activeGroup ? "给小组发消息…" : "说说你想做什么…"}/><button className="primary puck-send puck-icon-btn" type="button" aria-label="发送" onClick={() => void send()} disabled={activeGroup ? !draft.trim() : !draft.trim() && screenshots.length === 0}><svg className="puck-send-icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false"><path d="M10 15V5m0 0L6.5 8.5M10 5l3.5 3.5"/></svg></button></div></div></footer>}
    </section>

    <aside className={`puck-info ${mobilePanel === "info" ? "mobile-open" : ""}`}>
      <header><h3>助理信息</h3></header>
      {activeGroup ? <div className="puck-info-content"><section className="puck-info-details"><h4>小组成员</h4><ul className="puck-group-members">{activeGroup.members.map(member => <li key={member.assistantId}><PuckAvatar assistantId={member.assistantId} name={member.name} design={avatarDesign(assistants.find(item => item.assistantId === member.assistantId) || { assistantId: member.assistantId, name: member.name, brainDeviceId: "", status: "ready" })} size="small"/><span>{member.name}</span></li>)}</ul><p>每位成员会用自己的大脑简短回复。这会打断该成员正在进行的一对一回合。</p></section></div> : selected ? <div className="puck-info-content">
        <div className="puck-avatar-editor"><PuckAvatar assistantId={selected.assistantId} name={selected.name} design={avatarDesign(selected)} state={puckState} size="large"/><strong>{selected.name}</strong>{editingDescription ? <div className="puck-description-editor"><textarea aria-label="助理说明" maxLength={512} value={descriptionDraft} onChange={event=>setDescriptionDraft(event.target.value)}/><div><button className="puck-btn" type="button" onClick={()=>{setEditingDescription(false);setDescriptionDraft(selected.description || "");}}>取消</button><button className="puck-btn primary" type="button" aria-label="保存说明" onClick={()=>void saveDescription()}>保存</button></div></div> : <><p>{selected.description || "尚未添加说明"}</p><button className="puck-description-edit" type="button" aria-label="编辑说明" onClick={()=>{setDescriptionDraft(selected.description || "");setEditingDescription(true);}}>编辑说明</button></>}</div>
        <div className="puck-avatar-picker"><div className="puck-picker-section"><span className="puck-avatar-picker-label">形状</span><div className="puck-shape-options" role="radiogroup" aria-label="头像形状">{avatarShapes.map(option => { const checked = avatarDesign(selected).shape === option.value; return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-shape-option ${checked ? "selected" : ""}`} key={option.value} onClick={()=>void updateAvatar(option.value,avatarDesign(selected).color)} onKeyDown={event=>moveRadio(event,avatarShapes.map(item=>item.value),avatarDesign(selected).shape,value=>void updateAvatar(value as AvatarShape,avatarDesign(selected).color))}><PuckAvatar assistantId={`${selected.assistantId}-${option.value}`} name={option.label} design={{shape:option.value,color:avatarDesign(selected).color,colorHex:avatarDesign(selected).colorHex}} size="small" animated={false}/></button>; })}</div></div>
          <div className="puck-picker-section"><span className="puck-avatar-picker-label">颜色</span><div className="puck-color-options" role="radiogroup" aria-label="头像颜色">{avatarColors.map(option => { const checked = avatarDesign(selected).color === option.value; return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-color-option ${checked ? "selected" : ""}`} style={{"--puck-choice-color":option.hex} as CSSProperties} key={option.value} onClick={()=>void updateAvatar(avatarDesign(selected).shape,option.value)} onKeyDown={event=>moveRadio(event,avatarColors.map(item=>item.value),avatarDesign(selected).color,value=>void updateAvatar(avatarDesign(selected).shape,value as AvatarColor))}><span aria-hidden="true"/></button>; })}</div></div>
        </div>
		<section className="puck-info-details"><h4>详情</h4><dl><dt>名称</dt><dd>{selected.name}</dd><dt>状态</dt><dd><span className="puck-info-status" data-state={puckState}><i aria-hidden="true"/>{statusLabel}</span></dd><dt>大脑主机</dt><dd><span aria-hidden="true">▣</span> {devices.find(device => device.deviceId === selected.brainDeviceId)?.name || selected.brainDeviceId}</dd><dt>运行权限</dt><dd><label className="puck-permission-setting"><span className="sr-only">运行权限</span><select aria-label="运行权限" value={selected.permissionMode || "full"} onChange={event=>void updatePermissionMode(event.target.value as "full" | "ask")}><option value="full">Full Access（默认）</option><option value="ask">每次询问</option></select></label><small>{(selected.permissionMode || "full") === "full" ? "可执行任意命令并访问主机文件，不再询问" : "高风险操作会请求确认"}</small></dd></dl></section>
		<section className="puck-authorization-rules"><h4>以后都允许</h4>{authorizationRules.length === 0 ? <p>尚未保存规则</p> : <ul>{authorizationRules.map(rule=><li key={rule.id}><div><strong>{rule.actionType}</strong><span>{rule.deviceId}</span><code>{rule.cwd}</code><time dateTime={rule.createdAt}>{new Date(rule.createdAt).toLocaleString("zh-CN")}</time></div><button className="puck-btn" type="button" aria-label={`撤销规则 ${rule.actionType} ${rule.cwd}`} onClick={()=>void revokeAuthorizationRule(rule.id)}>撤销</button></li>)}</ul>}</section>
		<section className="puck-danger-zone"><h4>危险操作</h4><p>删除后，这个 Bot 及其历史将不再显示。</p><button className="puck-btn danger" type="button" onClick={()=>setDeleteTarget(selected)}>删除 Bot</button></section>
      </div> : <p className="puck-info-empty">选择助理后查看设置。</p>}
    </aside>

    {creatingGroup && <div className="puck-dialog-backdrop" role="presentation"><form className="puck-dialog" onSubmit={event => { event.preventDefault(); if (!createIme.blocksFormSubmit()) void createGroup(); }}><h2>建组聊天</h2><label>组名<input aria-label="组名" value={groupName} onChange={event => setGroupName(event.target.value)} maxLength={40} autoFocus/></label><fieldset className="puck-group-members"><legend>选择成员</legend>{assistants.map(item => <label key={item.assistantId}><input type="checkbox" checked={groupMemberIDs.includes(item.assistantId)} onChange={event => setGroupMemberIDs(current => event.target.checked ? [...current, item.assistantId] : current.filter(id => id !== item.assistantId))}/>{item.name}</label>)}</fieldset><div><button className="puck-btn puck-btn-md" type="button" onClick={() => setCreatingGroup(false)}>取消</button><button className="puck-btn puck-btn-md primary" type="submit" disabled={!groupName.trim() || groupMemberIDs.length < 2}>创建</button></div></form></div>}
    {creating && <div className="puck-dialog-backdrop" role="presentation"><form className="puck-dialog" onSubmit={event => { event.preventDefault(); if (!createIme.blocksFormSubmit()) void createAssistant(); }}><h2>新建助理</h2><label>助理名称<input aria-label="助理名称" value={name} onChange={event => setName(event.target.value)} onCompositionStart={createIme.compositionStart} onCompositionEnd={createIme.compositionEnd} onBlur={createIme.blur} onKeyDown={event=>{createIme.classify(event);}} autoFocus maxLength={80}/></label><label>大脑主机<select aria-label="大脑主机" value={brainDeviceID} onChange={event => setBrainDeviceID(event.target.value)}><option value="">请选择</option>{readyBrains.map(device => <option key={device.deviceId} value={device.deviceId}>{device.name || device.deviceId}</option>)}</select></label><div><button className="puck-btn puck-btn-md" type="button" onClick={() => setCreating(false)}>取消</button><button className="puck-btn puck-btn-md primary" type="submit" onPointerDown={createIme.allowExplicitFormSubmit}>创建</button></div></form></div>}
		{deleteTarget && <div className="puck-dialog-backdrop" role="presentation"><section className="puck-dialog puck-delete-dialog" role="dialog" aria-modal="true" aria-labelledby="puck-delete-title"><h2 id="puck-delete-title">删除 Bot</h2><p>将删除“{deleteTarget.name}”，停止该 Bot 的后台大脑，历史不再显示。</p><p className="puck-dialog-note">已派发的 Codex 任务不会被强制停止，仍可在 Ariel 会话中继续查看。</p><div><button className="puck-btn puck-btn-md" type="button" disabled={deleting} onClick={()=>setDeleteTarget(undefined)}>取消</button><button className="puck-btn puck-btn-md danger" type="button" aria-label={`确认删除${deleteTarget.name}`} disabled={deleting} onClick={()=>void deleteAssistant()}>{deleting ? "正在删除…" : "删除"}</button></div></section></div>}
  </main>;
}
