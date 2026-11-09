import type { Item } from "./generated/protocol";

export type SystemPresentation =
  | { kind: "automatic-approval"; title: string; details: string[] }
  | { kind: "unsupported"; title: string; details: string[] }
  | { kind: "steered"; title: string; details: [] };

// These prefixes are Adapter-owned v1 text projections. They intentionally
// avoid new Item fields so an older strict Web client can still show friendly
// plain text instead of rejecting the whole thread snapshot.
export function systemPresentation(item: Item): SystemPresentation | undefined {
  if (item.role !== "system" || item.activity) return;
  const [title = "", ...details] = item.text.split("\n").map(line => line.trim()).filter(Boolean);
  if (title === "已插入引导") return { kind: "steered", title, details: [] };
  if (title === "暂不支持显示的内容" && details.length > 0 && details.every(line => line.startsWith("类型："))) {
    return { kind: "unsupported", title, details };
  }
  if ((title.startsWith("已自动审批") || title.startsWith("自动审批检查")) && details.length > 0) {
    return { kind: "automatic-approval", title, details };
  }
}
