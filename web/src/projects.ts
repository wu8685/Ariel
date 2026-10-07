export type ProjectGroup<T> = {
  key: string;
  name: string;
  path: string;
  threads: T[];
};

export function normalizeProjectPath(cwd: string): string {
  if (!cwd.trim()) return "";
  const path = cwd;
  if (path === "/") return path;
  if (/^[A-Za-z]:[\\/]+$/.test(path)) return `${path.slice(0, 2)}${path.includes("\\") ? "\\" : "/"}`;
  return path.replace(/[\\/]+$/, "");
}

export function projectName(cwd: string): string {
  const path = normalizeProjectPath(cwd);
  if (!path) return "未知项目";
  if (path === "/" || /^[A-Za-z]:[\\/]$/.test(path)) return path;
  return path.split(/[\\/]/).filter(Boolean).at(-1) || path;
}

export function groupThreadsByProject<T extends { cwd: string }>(threads: readonly T[]): ProjectGroup<T>[] {
  const groups: ProjectGroup<T>[] = [];
  const byPath = new Map<string, ProjectGroup<T>>();
  for (const thread of threads) {
    const normalized = normalizeProjectPath(thread.cwd);
    const key = normalized || "\u0000unknown-project";
    let group = byPath.get(key);
    if (!group) {
      group = { key, name: projectName(normalized), path: normalized || "路径未知", threads: [] };
      byPath.set(key, group);
      groups.push(group);
    }
    group.threads.push(thread);
  }
  return groups;
}

export function partitionThreadsByPin<T extends { isPinned?: boolean }>(threads: readonly T[]): { pinned: T[]; regular: T[] } {
  const pinned: T[] = [];
  const regular: T[] = [];
  for (const thread of threads) (thread.isPinned === true ? pinned : regular).push(thread);
  return { pinned, regular };
}

export function mergeThreadPages<T extends { threadId: string }>(current: readonly T[], incoming: readonly T[]): T[] {
  const updates = new Map(incoming.map(thread => [thread.threadId, thread]));
  const merged = current.map(thread => updates.get(thread.threadId) || thread);
  const known = new Set(current.map(thread => thread.threadId));
  for (const thread of incoming) if (!known.has(thread.threadId)) merged.push(thread);
  return merged;
}
