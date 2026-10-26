import type { Item } from "./generated/protocol";

// Readable, text-only Adapter projection (0113). Do not add unnegotiated fields
// to v1 Items: older, long-lived Web clients validate them strictly (0109).
// This affects presentation only, never execution state or mutation gating.
export function nativeNoticeKind(item: Item): "retry" | "failure" | "unknown" | undefined {
  if (item.role !== "system" || item.activity) return;
  if (item.text.startsWith("Codex 正在重试：")) return "retry";
  if (item.text.startsWith("Codex 错误：")) return "failure";
  if (item.text.startsWith("Codex 状态未确认：")) return "unknown";
}
