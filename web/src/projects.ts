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
