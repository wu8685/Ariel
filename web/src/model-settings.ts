export type ExecutionSpeed = { mode: "inherit" | "standard" | "tier"; tierId?: string };
export type ExecutionValues = { model: string; effort: string; speed: ExecutionSpeed };
export type ExecutionSnapshot = { revision: string; values: ExecutionValues; freshness: "fresh" | "stale"; writable: boolean };
export type ExecutionModel = { id: string; name: string; description: string; efforts: { id: string; description: string }[]; defaultEffort: string; tiers: { id: string; name: string; description: string }[]; defaultTier: string | null; inputModalities: string[]; isDefault: boolean };
export type ExecutionCatalog = { models: ExecutionModel[] };
export type ExecutionGuard = { key: string; busy: boolean; imagesBlocked: boolean; draftSettings?: ExecutionValues };
const record = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const str = (v: unknown, max = 256): v is string => typeof v === "string" && v.length <= max;
export function parseCatalog(v: unknown): ExecutionCatalog | null {
  if (!record(v) || !Array.isArray(v.models) || v.models.length > 512 || JSON.stringify(v).length > 1 << 20) return null;
  const ids = new Set<string>();
  for (const m of v.models) {
    if (!record(m) || !str(m.id) || !m.id || ids.has(m.id) || !str(m.name) || !str(m.description, 2048) || !str(m.defaultEffort, 128) || typeof m.isDefault !== "boolean"
      || !Array.isArray(m.efforts) || m.efforts.length > 32 || !m.efforts.every(e => record(e) && str(e.id, 128) && e.id && str(e.description, 2048))
      || !Array.isArray(m.tiers) || m.tiers.length > 16 || !m.tiers.every(t => record(t) && str(t.id, 128) && t.id && str(t.name) && str(t.description, 2048))
      || !Array.isArray(m.inputModalities) || m.inputModalities.length > 8 || !m.inputModalities.every(i => str(i, 128)) || m.defaultTier !== null && !str(m.defaultTier, 128)) return null;
    ids.add(m.id);
  }
  return v as ExecutionCatalog;
}
export function parseSettings(v: unknown): ExecutionSnapshot | null {
  if (!record(v) || !str(v.revision, 128) || !v.revision || typeof v.writable !== "boolean" || (v.freshness !== "fresh" && v.freshness !== "stale") || !record(v.values)) return null;
  const x = v.values, speed = x.speed;
  if (!str(x.model) || !str(x.effort, 128) || !record(speed) || !["inherit", "standard", "tier"].includes(String(speed.mode)) || (speed.mode === "tier" ? !str(speed.tierId, 128) || !speed.tierId : speed.tierId !== undefined)) return null;
  return v as ExecutionSnapshot;
}
export function changeModel(current: ExecutionValues, model: ExecutionModel): { values: ExecutionValues; changedDefaults: boolean } {
  const effort = !current.effort || model.efforts.some(e => e.id === current.effort) ? current.effort : model.defaultEffort;
  const speed: ExecutionSpeed = current.speed.mode === "tier" && !model.tiers.some(t => t.id === current.speed.tierId) ? { mode: "inherit" } : current.speed;
  return { values: { model: model.id, effort, speed }, changedDefaults: effort !== current.effort || speed !== current.speed };
}
export function effortLabel(id: string): string { return ({ none: "无", minimal: "最小", low: "低", medium: "中", high: "高", xhigh: "超高", max: "最高", ultra: "Ultra" } as Record<string, string>)[id] || id || "继承 Codex"; }
export function speedLabel(speed: ExecutionSpeed, model?: ExecutionModel): string { return speed.mode === "standard" ? "标准" : speed.mode === "inherit" ? "继承 Codex" : model?.tiers.find(t => t.id === speed.tierId)?.name || speed.tierId || "未确认"; }
export function defaultValues(catalog: ExecutionCatalog): ExecutionValues | null { const m = catalog.models.find(m => m.isDefault) || catalog.models[0]; return m ? { model: m.id, effort: m.defaultEffort, speed: { mode: "inherit" } } : null; }
// Bounded per-page projections; Codex remains the settings SSOT.
export class ExecutionSettingsStore {
  readonly snapshots = new Map<string, ExecutionSnapshot>(); readonly drafts = new Map<string, ExecutionValues>();
  private catalogs = new Map<string, { until: number; promise: Promise<ExecutionCatalog | null> }>();
  keep<T>(map: Map<string, T>, key: string, value: T) { map.delete(key); map.set(key, value); if (map.size > 64) map.delete(map.keys().next().value!); }
  catalog(key: string, read: () => Promise<ExecutionCatalog | null>, refresh = false): Promise<ExecutionCatalog | null> {
    const cached = this.catalogs.get(key); if (!refresh && cached && cached.until > Date.now()) return cached.promise;
    const promise = read().then(result => { if (!result && this.catalogs.get(key)?.promise === promise) this.catalogs.delete(key); return result; },error=>{if(this.catalogs.get(key)?.promise===promise)this.catalogs.delete(key);throw error;});
    this.catalogs.set(key, { until: Date.now() + 300_000, promise }); if (this.catalogs.size > 8) this.catalogs.delete(this.catalogs.keys().next().value!); return promise;
  }
}
