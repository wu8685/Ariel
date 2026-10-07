export type QueueRowGeometry = { queueId: string; top: number; bottom: number };

export function reorderQueueAtPointer(order: readonly string[], draggedID: string, pointerY: number, rows: readonly QueueRowGeometry[]): string[] {
  if (!Number.isFinite(pointerY) || !draggedID || order.filter(id => id === draggedID).length !== 1) return [...order];
  const remaining = order.filter(id => id !== draggedID);
  const remainingIDs = new Set(remaining);
  const positioned = rows
    .filter(row => row.queueId !== draggedID && remainingIDs.has(row.queueId) && Number.isFinite(row.top) && Number.isFinite(row.bottom) && row.bottom >= row.top)
    .sort((left, right) => left.top - right.top);
  const positionedIDs = positioned.map(row => row.queueId);
  if (positionedIDs.length !== remaining.length || new Set(positionedIDs).size !== remaining.length || remaining.some(id => !positionedIDs.includes(id))) return [...order];
  const insertion = positioned.findIndex(row => pointerY < (row.top + row.bottom) / 2);
  positionedIDs.splice(insertion < 0 ? positionedIDs.length : insertion, 0, draggedID);
  return positionedIDs;
}
