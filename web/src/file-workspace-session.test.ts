// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { FileWorkspaceModel, type FileTarget } from "./file-workspace";
import {
  FileWorkspaceSession,
  fileWorkspaceSessionKey,
  decodeFileWorkspace,
} from "./file-workspace-session";

const target = (path = "a.md"): FileTarget => ({
  deviceId: "mac",
  agentEpoch: "e",
  threadId: "t",
  path,
  source: "workspace_browser",
});
const device = {
  deviceId: "mac",
  agentEpoch: "e",
  capabilities: {
    chillingFileContext: true,
    chillingWorkspaceBrowser: true,
    chillingMessageReferences: true,
  },
};
function storage() {
  const data = new Map<string, string>();
  return {
    data,
    getItem: vi.fn((k: string) => data.get(k) || null),
    setItem: vi.fn((k: string, v: string) => {
      data.set(k, v);
    }),
    removeItem: vi.fn((k: string) => {
      data.delete(k);
    }),
  };
}
afterEach(() => {
  vi.useRealTimers();
});

it("saves only allowlisted metadata, restores 150 descriptors lazily after authorization, and deduplicates", () => {
  const store = storage(),
    model = new FileWorkspaceModel();
  const session = new FileWorkspaceSession(model, () => store);
  const stop = session.start(vi.fn());
  for (let i = 0; i < 150; i++) model.open(target(`file-${i}.md`));
  const tab = model.tabs.at(-1)!;
  Object.assign(tab.reader, {
    reading: true,
    sha256: "a".repeat(64),
    selection: { start: 2, end: 8 },
    positions: { source: 15, reading: 44, diff: 0 },
    body: "PRIVATE_BODY",
    token: "SECRET_TOKEN",
  });
  model.readerChanged(tab.id);
  model.collapse();
  window.dispatchEvent(new Event("pagehide"));
  const raw = store.data.get(fileWorkspaceSessionKey)!;
  expect(raw).not.toContain("PRIVATE_BODY");
  expect(raw).not.toContain("SECRET_TOKEN");
  expect(raw).not.toContain("generation");
  expect(raw).not.toContain("identity");
  stop();
  const restored = new FileWorkspaceModel(),
    recovery = new FileWorkspaceSession(restored, () => store);
  recovery.start(vi.fn());
  expect(restored.tabs).toHaveLength(0);
  recovery.restoreWhenReady(false, "mac", [device], true);
  expect(restored.tabs).toHaveLength(0);
  expect(recovery.acceptDevices([device], true)).toBe("mac");
  recovery.restoreWhenReady(true, "mac", [device], true);
  expect(restored.tabs).toHaveLength(150);
  expect(restored.collapsed).toBe(true);
  expect(restored.resourceCount).toBe(0);
  expect(restored.tabs.at(-1)!.reader).toMatchObject({
    reading: true,
    selection: { start: 2, end: 8 },
    positions: { source: 15, reading: 44, diff: 0 },
  });
  expect(restored.open(target("file-149.md"))).toBe(tab.id);
  expect(restored.tabs).toHaveLength(150);
  recovery.restoreWhenReady(true, "mac", [device], true);
  expect(restored.tabs).toHaveLength(150);
  recovery.stop();
});

it("debounces reader-only changes and flushes visibility/pagehide without letting clear resurrect a checkpoint", () => {
  vi.useFakeTimers();
  const store = storage(),
    model = new FileWorkspaceModel(),
    session = new FileWorkspaceSession(model, () => store);
  session.start(vi.fn());
  const id = model.open(target());
  model.tabs[0].reader.positions.source = 345;
  model.readerChanged(id);
  vi.advanceTimersByTime(249);
  expect(store.setItem).not.toHaveBeenCalled();
  vi.advanceTimersByTime(1);
  expect(store.setItem).toHaveBeenCalledTimes(1);
  expect(
    JSON.parse(store.data.get(fileWorkspaceSessionKey)!).tabs[0].reader
      .positions.source,
  ).toBe(345);
  model.collapse();
  window.dispatchEvent(new Event("pagehide"));
  expect(JSON.parse(store.data.get(fileWorkspaceSessionKey)!).collapsed).toBe(
    true,
  );
  model.expand();
  session.clear();
  vi.runAllTimers();
  window.dispatchEvent(new Event("pagehide"));
  expect(store.data.has(fileWorkspaceSessionKey)).toBe(false);
  expect(model.tabs).toHaveLength(0);
  session.stop();
});

it.each(["epoch", "capability", "device", "flag"])(
  "discards untrusted %s restore without displaying paths",
  (reason) => {
    const store = storage(),
      model = new FileWorkspaceModel(),
      session = new FileWorkspaceSession(model, () => store);
    session.start(vi.fn());
    model.open(target());
    session.flush();
    session.stop();
    const next = new FileWorkspaceModel(),
      restore = new FileWorkspaceSession(next, () => store);
    restore.start(vi.fn());
    restore.acceptDevices(
      reason === "device"
        ? []
        : [
            {
              ...device,
              agentEpoch: reason === "epoch" ? "other" : "e",
              capabilities: {
                ...device.capabilities,
                chillingWorkspaceBrowser: reason !== "capability",
              },
            },
          ],
      reason !== "flag",
    );
    expect(next.tabs).toHaveLength(0);
    expect(store.data.has(fileWorkspaceSessionKey)).toBe(false);
    restore.stop();
  },
);

it("keeps pending metadata across empty initial models, transient auth loss and StrictMode restarts", () => {
  const store = storage(),
    initial = new FileWorkspaceModel(),
    first = new FileWorkspaceSession(initial, () => store);
  first.start(vi.fn());
  initial.open(target());
  first.flush();
  first.stop();
  const model = new FileWorkspaceModel(),
    session = new FileWorkspaceSession(model, () => store);
  session.start(vi.fn());
  model.clear();
  session.stop();
  session.start(vi.fn());
  session.flush();
  expect(store.data.has(fileWorkspaceSessionKey)).toBe(true);
  session.restoreWhenReady(false, "mac", [device], true);
  expect(model.tabs).toHaveLength(0);
  session.restoreWhenReady(true, "mac", [device], true);
  expect(model.tabs).toHaveLength(1);
  session.stop();
});

it("degrades quota, blocked storage and oversize checkpoints without losing live tabs or leaving stale restore data", () => {
  const store = storage(),
    model = new FileWorkspaceModel(),
    warn = vi.fn(),
    session = new FileWorkspaceSession(model, () => store);
  session.start(warn);
  model.open(target());
  session.flush();
  store.setItem.mockImplementation(() => {
    throw new Error("quota");
  });
  model.open(target("b.md"));
  session.flush();
  expect(model.tabs).toHaveLength(2);
  expect(store.data.has(fileWorkspaceSessionKey)).toBe(false);
  expect(warn).toHaveBeenCalledWith(expect.stringContaining("无法保存"));
  session.stop();
  const blocked = new FileWorkspaceSession(model, () => {
    throw new Error("blocked");
  });
  blocked.start(warn);
  expect(() => blocked.flush()).not.toThrow();
  blocked.stop();
  const large = new FileWorkspaceModel(),
    big = new FileWorkspaceSession(large, () => store);
  big.start(warn);
  for (let i = 0; i < 350; i++)
    large.open(target(`${"文".repeat(4000)}${i}.md`));
  big.flush();
  expect(large.tabs).toHaveLength(350);
  expect(store.data.has(fileWorkspaceSessionKey)).toBe(false);
  big.stop();
});

it("rejects invalid, expired, duplicate, unsafe and oversized metadata", () => {
  const store = storage(),
    model = new FileWorkspaceModel(),
    session = new FileWorkspaceSession(model, () => store);
  session.start(vi.fn());
  model.open(target());
  session.flush();
  session.stop();
  const raw = store.data.get(fileWorkspaceSessionKey)!;
  const valid = JSON.parse(raw);
  expect(decodeFileWorkspace(raw)).not.toBeNull();
  for (const mutate of [
    (v: any) => {
      v.version = 2;
    },
    (v: any) => {
      v.savedAt = 0;
    },
    (v: any) => {
      v.tabs.push(v.tabs[0]);
    },
    (v: any) => {
      v.tabs[0].target.path = "../escape.md";
    },
    (v: any) => {
      v.tabs[0].target.threadId = "x".repeat(129);
    },
    (v: any) => {
      v.tabs[0].reader.positions.source = -1;
    },
    (v: any) => {
      v.tabs[0].reader.selection = { start: 5, end: 2 };
    },
    (v: any) => {
      v.tabs[0].target.source = "message_reference";
    },
    (v: any) => {
      v.activeId = "missing";
    },
    (v: any) => {
      v.tabs[0].reader.sha256 = "bad";
    },
    (v: any) => {
      v.tabs[0].target.deviceId = "another";
    },
  ]) {
    const value = structuredClone(valid);
    mutate(value);
    expect(decodeFileWorkspace(JSON.stringify(value))).toBeNull();
  }
  expect(decodeFileWorkspace("{")).toBeNull();
  expect(decodeFileWorkspace(" ".repeat(1024 * 1024 + 1))).toBeNull();
});

it("rejects URL payloads masquerading as file references and strips unknown fields", () => {
  const store = storage(),
    model = new FileWorkspaceModel(),
    session = new FileWorkspaceSession(model, () => store);
  session.start(vi.fn());
  model.open({
    ...target(),
    source: "message_reference",
    turnId: "turn",
    itemId: "item",
    referenceKind: "markdown_link",
    rawReference: "a.md:2",
  });
  session.flush();
  session.stop();
  const checkpoint = JSON.parse(store.data.get(fileWorkspaceSessionKey)!);
  for (const rawReference of [
    "data:image/png;base64,PRIVATE_BODY.md",
    "javascript:private.md",
    "https://host.test/a.md",
    "file:///tmp/a.md",
    "../outside.md",
  ]) {
    const copy = structuredClone(checkpoint);
    copy.tabs[0].target.rawReference = rawReference;
    expect(decodeFileWorkspace(JSON.stringify(copy)), rawReference).toBeNull();
  }
  checkpoint.tabs[0].reader.text = "PRIVATE_QUOTE";
  checkpoint.tabs[0].target.secret = "SECRET";
  expect(
    JSON.stringify(decodeFileWorkspace(JSON.stringify(checkpoint))),
  ).not.toContain("PRIVATE_QUOTE");
  expect(
    JSON.stringify(decodeFileWorkspace(JSON.stringify(checkpoint))),
  ).not.toContain("SECRET");
});
