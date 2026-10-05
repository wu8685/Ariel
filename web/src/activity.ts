import type { Item, Turn } from "./generated/protocol";

export type ConversationPart =
  | { kind: "message"; item: Item }
  | { kind: "activity"; items: Item[]; firstLoaded: boolean };

// Only Adapter-classified items carry activity. Legacy and unknown system items
// remain visible until a compatible Agent provides a verified classification.
export function groupTurnItems(turn: Turn): ConversationPart[] {
  const parts: ConversationPart[] = [];
  for (const item of turn.items) {
    if (item.activity) {
      const last = parts.at(-1);
      if (last?.kind === "activity") last.items.push(item);
      else parts.push({ kind: "activity", items: [item], firstLoaded: parts.length === 0 && turn.itemsComplete === false });
    } else if (item.role !== "assistant" || item.text) parts.push({ kind: "message", item });
  }
  return parts;
}

export function activityStatusText(status: NonNullable<Item["activity"]>["status"]): string {
  return ({ inProgress: "运行中", completed: "已完成", failed: "失败", interrupted: "已停止", unknown: "状态未知" } as const)[status];
}
