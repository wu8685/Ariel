import { describe, expect, it } from "vitest";
import { groupThreadsByProject, normalizeProjectPath, projectName } from "./projects";

type Session = { threadId: string; cwd: string };

describe("project session grouping", () => {
  it("groups by normalized full cwd while preserving project and thread order", () => {
    const sessions: Session[] = [
      { threadId: "a-new", cwd: "/work/alpha/" },
      { threadId: "b", cwd: "/work/beta" },
      { threadId: "a-old", cwd: "/work/alpha" },
    ];
    expect(groupThreadsByProject(sessions)).toEqual([
      { key: "/work/alpha", name: "alpha", path: "/work/alpha", threads: [sessions[0], sessions[2]] },
      { key: "/work/beta", name: "beta", path: "/work/beta", threads: [sessions[1]] },
    ]);
  });

  it("keeps same-named directories with different full paths separate", () => {
    const groups = groupThreadsByProject<Session>([
      { threadId: "one", cwd: "/work/app" },
      { threadId: "two", cwd: "/archive/app" },
    ]);
    expect(groups.map(group => [group.name, group.path])).toEqual([["app", "/work/app"], ["app", "/archive/app"]]);
  });

  it("handles POSIX and Windows roots, trailing separators, and missing cwd", () => {
    expect(normalizeProjectPath("/")).toBe("/");
    expect(normalizeProjectPath("C:\\\\")).toBe("C:\\");
    expect(normalizeProjectPath("/work/app///")).toBe("/work/app");
    expect(normalizeProjectPath("C:\\work\\app\\")).toBe("C:\\work\\app");
    expect(normalizeProjectPath("  ")).toBe("");
    expect(projectName("/")).toBe("/");
    expect(projectName("C:\\")).toBe("C:\\");
    expect(projectName("")).toBe("未知项目");
  });
});
