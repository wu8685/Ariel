import type { FileReaderState } from "./file-context";
import {
  fileTargetKey,
  type FileTarget,
  type FileWorkspaceModel,
  type FileWorkspaceState,
} from "./file-workspace";

export const fileWorkspaceSessionKey = "ariel.file-workspace.v1";
const maxBytes = 1024 * 1024,
  ttl = 7 * 24 * 60 * 60 * 1000;
const encoder = new TextEncoder();
type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">;
export type FileAccessDevice = {
  deviceId: string;
  agentEpoch?: string;
  capabilities: {
    chillingFileContext?: boolean;
    chillingWorkspaceBrowser?: boolean;
    chillingMessageReferences?: boolean;
  };
};
type Checkpoint = FileWorkspaceState & {
  version: 1;
  savedAt: number;
  deviceId: string;
  agentEpoch: string;
};
const object = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const text = (v: unknown, max = 128): v is string =>
  typeof v === "string" &&
  v.length > 0 &&
  v.length <= max &&
  !/[\u0000-\u001f\u007f-\u009f]/u.test(v);
const position = (v: unknown): v is number =>
  typeof v === "number" &&
  Number.isFinite(v) &&
  v >= 0 &&
  v <= Number.MAX_SAFE_INTEGER;
const line = (v: unknown): v is number =>
  position(v) && Number.isInteger(v) && v > 0;
const hash = (v: unknown): v is string =>
  typeof v === "string" && /^[0-9a-f]{64}$/u.test(v);
const path = (v: unknown): v is string =>
  text(v, 4096) &&
  !/^(?:[/\\]|[a-z][a-z\d+.-]*:)/iu.test(v) &&
  !v.includes("\\") &&
  !v.split("/").some((part) => part === ".." || part === ".git");
function reference(v: unknown): v is string {
  if (!text(v, 4096)) return false;
  const file = v.replace(
    /(?::[1-9]\d*(?::[1-9]\d*)?|#L[1-9]\d*(?:-L[1-9]\d*)?)$/u,
    "",
  );
  return (
    !/^(?:[a-z][a-z\d+.-]*:|\/\/|#)/iu.test(file) &&
    !file.includes("\\") &&
    !file.split("/").some((part) => part === ".." || part === ".git") &&
    /(?:^|\/)[^/]+\.[\p{L}\p{N}]{1,16}$/u.test(file)
  );
}

function target(value: unknown): FileTarget | null {
  if (
    !object(value) ||
    !text(value.deviceId) ||
    !text(value.agentEpoch) ||
    !text(value.threadId) ||
    !text(value.path, 4096)
  )
    return null;
  const { deviceId, agentEpoch, threadId } = value;
  if (
    !["workspace_browser", "turn_change", "message_reference"].includes(
      String(value.source),
    )
  )
    return null;
  const source = value.source as FileTarget["source"];
  if (source !== "message_reference" && !path(value.path)) return null;
  if (source === "message_reference" && !path(value.path) && !reference(value.path)) return null;
  if (
    source !== "workspace_browser" &&
    (!text(value.turnId) || !text(value.itemId))
  )
    return null;
  if (
    source === "message_reference" &&
    (!["markdown_link", "plain_path"].includes(String(value.referenceKind)) ||
      !reference(value.rawReference))
  )
    return null;
  if (
    source === "workspace_browser" &&
    (value.turnId !== undefined ||
      value.itemId !== undefined ||
      value.rawReference !== undefined ||
      value.referenceKind !== undefined)
  )
    return null;
  if (
    source === "turn_change" &&
    (value.rawReference !== undefined || value.referenceKind !== undefined)
  )
    return null;
  if (
    value.kind !== undefined &&
    !["add", "update", "delete"].includes(String(value.kind))
  )
    return null;
  if (
    (value.focusStartLine !== undefined && !line(value.focusStartLine)) ||
    (value.focusEndLine !== undefined &&
      (!line(value.focusEndLine) ||
        !line(value.focusStartLine) ||
        value.focusEndLine < value.focusStartLine))
  )
    return null;
  return {
    deviceId,
    agentEpoch,
    threadId,
    path: value.path,
    source,
    ...(source !== "workspace_browser"
      ? { turnId: value.turnId as string, itemId: value.itemId as string }
      : {}),
    ...(source === "message_reference"
      ? {
          referenceKind: value.referenceKind as FileTarget["referenceKind"],
          rawReference: value.rawReference as string,
        }
      : {}),
    ...(value.kind ? { kind: value.kind as FileTarget["kind"] } : {}),
    ...(value.focusStartLine
      ? { focusStartLine: value.focusStartLine as number }
      : {}),
    ...(value.focusEndLine
      ? { focusEndLine: value.focusEndLine as number }
      : {}),
  };
}
function reader(value: unknown): FileReaderState | null {
  if (
    !object(value) ||
    typeof value.reading !== "boolean" ||
    !object(value.positions)
  )
    return null;
  const { source, reading, diff } = value.positions;
  if (!position(source) || !position(reading) || !position(diff)) return null;
  if (value.sha256 !== undefined && !hash(value.sha256)) return null;
  if (
    value.diffFingerprint !== undefined &&
    (typeof value.diffFingerprint !== "string" ||
      !/^[0-9a-f]{32}$/u.test(value.diffFingerprint))
  )
    return null;
  let selection: FileReaderState["selection"];
  if (value.selection !== undefined) {
    const s = value.selection;
    if (
      !object(s) ||
      !Number.isSafeInteger(s.start) ||
      !Number.isSafeInteger(s.end) ||
      (s.start as number) < 0 ||
      (s.end as number) <= (s.start as number) ||
      !hash(value.sha256)
    )
      return null;
    selection = { start: s.start as number, end: s.end as number };
  }
  return {
    reading: value.reading,
    positions: { source, reading, diff },
    ...(value.sha256 ? { sha256: value.sha256 as string } : {}),
    ...(value.diffFingerprint
      ? { diffFingerprint: value.diffFingerprint as string }
      : {}),
    ...(selection ? { selection } : {}),
    ...(typeof value.focusApplied === "boolean"
      ? { focusApplied: value.focusApplied }
      : {}),
  };
}
/** Untrusted browser input is reconstructed, never spread into the live model. */
export function decodeFileWorkspace(
  raw: string,
  now = Date.now(),
): Checkpoint | null {
  if (raw.length > maxBytes || encoder.encode(raw).byteLength > maxBytes)
    return null;
  try {
    const v: unknown = JSON.parse(raw);
    if (
      !object(v) ||
      v.version !== 1 ||
      !position(v.savedAt) ||
      v.savedAt > now + 60_000 ||
      now - v.savedAt > ttl ||
      !text(v.deviceId) ||
      !text(v.agentEpoch) ||
      typeof v.collapsed !== "boolean" ||
      !text(v.activeId) ||
      !Array.isArray(v.tabs) ||
      !v.tabs.length
    )
      return null;
    const tabs: FileWorkspaceState["tabs"] = [],
      ids = new Set<string>(),
      keys = new Set<string>();
    for (const rawTab of v.tabs) {
      if (
        !object(rawTab) ||
        !text(rawTab.id) ||
        !/^file-tab-[1-9]\d{0,12}$/u.test(rawTab.id) ||
        ids.has(rawTab.id) ||
        !["source", "diff"].includes(String(rawTab.mode)) ||
        typeof rawTab.hasDiff !== "boolean"
      )
        return null;
      const t = target(rawTab.target),
        r = reader(rawTab.reader);
      if (
        !t ||
        !r ||
        t.deviceId !== v.deviceId ||
        t.agentEpoch !== v.agentEpoch ||
        keys.has(fileTargetKey(t)) ||
        (rawTab.mode === "diff" &&
          (t.source !== "turn_change" || !rawTab.hasDiff)) ||
        (t.kind === "delete" && rawTab.mode !== "diff")
      )
        return null;
      ids.add(rawTab.id);
      keys.add(fileTargetKey(t));
      // identity is derived from validated provenance, not trusted from storage.
      r.identity = JSON.stringify([
        t.threadId,
        t.turnId || "",
        t.itemId || "",
        t.path,
      ]);
      tabs.push({
        id: rawTab.id,
        target: t,
        mode: rawTab.mode as "source" | "diff",
        hasDiff: rawTab.hasDiff,
        reader: r,
      });
    }
    if (!ids.has(v.activeId)) return null;
    return {
      version: 1,
      savedAt: v.savedAt,
      deviceId: v.deviceId,
      agentEpoch: v.agentEpoch,
      tabs,
      activeId: v.activeId,
      collapsed: v.collapsed,
    };
  } catch {
    return null;
  }
}
function permitted(t: FileTarget, d: FileAccessDevice, enabled: boolean) {
  return (
    enabled &&
    d.capabilities.chillingFileContext === true &&
    (t.source === "workspace_browser"
      ? d.capabilities.chillingWorkspaceBrowser === true
      : t.source === "message_reference"
        ? d.capabilities.chillingMessageReferences === true
        : true)
  );
}

/** Page-scoped metadata only. Pending checkpoints cannot be overwritten by an empty startup model. */
export class FileWorkspaceSession {
  private pending: Checkpoint | null = null;
  private initialized = false;
  private dirty = false;
  private clearing = false;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private unsubscribe: (() => void) | undefined;
  private warn: (message: string) => void = () => {};
  constructor(
    private model: FileWorkspaceModel,
    private storage: () => Store,
  ) {}
  start(warn: (message: string) => void) {
    this.warn = warn;
    if (!this.initialized) {
      this.initialized = true;
      try {
        const raw = this.storage().getItem(fileWorkspaceSessionKey);
        if (raw) {
          this.pending = decodeFileWorkspace(raw);
          if (!this.pending) this.remove();
        }
      } catch {
        this.failed();
      }
    }
    if (!this.unsubscribe) {
      this.unsubscribe = this.model.subscribeReader(this.changed);
      window.addEventListener("pagehide", this.flush);
      document.addEventListener("visibilitychange", this.visibility);
    }
    return () => this.stop();
  }
  stop() {
    this.flush();
    this.cancel();
    this.unsubscribe?.();
    this.unsubscribe = undefined;
    window.removeEventListener("pagehide", this.flush);
    document.removeEventListener("visibilitychange", this.visibility);
  }
  private visibility = () => {
    if (document.visibilityState === "hidden") this.flush();
  };
  private cancel() {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
  }
  private changed = () => {
    if (this.clearing || (this.pending && !this.model.tabs.length)) return;
    // A user explicitly opening a new file supersedes a deferred restore.
    this.pending = null;
    this.dirty = true;
    if (this.timer === undefined) this.timer = setTimeout(this.flush, 250);
  };
  private remove() {
    try {
      this.storage().removeItem(fileWorkspaceSessionKey);
    } catch {
      /* best effort; inaccessible storage never becomes auth failure */
    }
  }
  private failed() {
    this.remove();
    this.warn(
      "无法保存文件阅读现场；当前文件仍可阅读，刷新后可能需要重新打开。",
    );
  }
  flush = () => {
    this.cancel();
    if (!this.dirty || this.pending || this.clearing) return;
    this.dirty = false;
    if (!this.model.tabs.length) {
      this.remove();
      return;
    }
    try {
      const first = this.model.tabs[0].target;
      const value = {
        version: 1,
        savedAt: Date.now(),
        deviceId: first.deviceId,
        agentEpoch: first.agentEpoch,
        activeId: this.model.activeId,
        collapsed: this.model.collapsed,
        tabs: this.model.tabs.map((tab) => ({
          id: tab.id,
          target: target(tab.target),
          mode: tab.mode,
          hasDiff: tab.hasDiff,
          reader: reader(tab.reader),
        })),
      };
      // reader()/target() are allowlists: no file body, quote text, credentials or runtime receipts.
      const raw = JSON.stringify(value);
      if (
        encoder.encode(raw).byteLength > maxBytes ||
        !decodeFileWorkspace(raw)
      )
        throw new Error("invalid checkpoint");
      this.storage().setItem(fileWorkspaceSessionKey, raw);
      this.warn("");
    } catch {
      this.failed();
    }
  };
  clear() {
    this.cancel();
    this.pending = null;
    this.dirty = false;
    this.clearing = true;
    this.model.clear();
    this.clearing = false;
    this.remove();
    this.warn("");
  }
  /** Only call with an authenticated, authoritative device.list reply. */
  acceptDevices(devices: FileAccessDevice[], enabled: boolean) {
    const tabs = this.pending?.tabs || this.model.tabs;
    if (!tabs.length) return "";
    const first = tabs[0].target,
      d = devices.find((d) => d.deviceId === first.deviceId);
    if (
      !d ||
      (d.agentEpoch && d.agentEpoch !== first.agentEpoch) ||
      tabs.some((tab) => !permitted(tab.target, d, enabled))
    ) {
      this.clear();
      return "";
    }
    return this.pending?.deviceId || "";
  }
  restoreWhenReady(
    authenticated: boolean,
    deviceId: string,
    devices: FileAccessDevice[],
    enabled: boolean,
  ) {
    if (!authenticated || !this.pending || deviceId !== this.pending.deviceId)
      return;
    const d = devices.find((d) => d.deviceId === deviceId);
    if (!d?.agentEpoch) return;
    const state = this.pending;
    if (
      d.agentEpoch !== state.agentEpoch ||
      state.tabs.some((tab) => !permitted(tab.target, d, enabled))
    ) {
      this.clear();
      return;
    }
    this.pending = null;
    this.model.restore(state);
  }
}
