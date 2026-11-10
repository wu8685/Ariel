import { useEffect, useRef, useState, type ChangeEvent, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { avatarColors, avatarShapes, PuckAvatar, type AvatarColor, type AvatarDesign, type AvatarShape } from "./PuckAvatar";

const shapeRows = [avatarShapes.slice(0, 4), avatarShapes.slice(4, 8), avatarShapes.slice(8)];
const colorRows = [avatarColors.slice(0, 6), avatarColors.slice(6)];
const tabs = [
  { id: "shape", label: "形状" },
  { id: "generate", label: "生成" },
  { id: "upload", label: "上传" },
] as const;
type AvatarTab = (typeof tabs)[number]["id"];

function moveRadio(event: ReactKeyboardEvent<HTMLButtonElement>, values: readonly string[], current: string, select: (value: string) => void) {
  let index = values.indexOf(current);
  if (["ArrowRight", "ArrowDown"].includes(event.key)) index = (index + 1) % values.length;
  else if (["ArrowLeft", "ArrowUp"].includes(event.key)) index = (index - 1 + values.length) % values.length;
  else if (event.key === "Home") index = 0;
  else if (event.key === "End") index = values.length - 1;
  else return;
  event.preventDefault();
  const next = values[index];
  const group = event.currentTarget.closest('[role="radiogroup"]');
  select(next);
  window.requestAnimationFrame(() => group?.querySelector<HTMLButtonElement>(`[data-avatar-choice="${next}"]`)?.focus());
}

export function PuckAvatarSheet({ assistantId, name, design, onClose, onShape, onColor, onReset, onUpload, onClearImage }: {
  assistantId: string;
  name: string;
  design: AvatarDesign;
  onClose: () => void;
  onShape: (shape: AvatarShape) => void;
  onColor: (color: AvatarColor) => void;
  onReset: () => void;
  onUpload: (file: File) => Promise<void>;
  onClearImage: () => void;
}) {
  const [tab, setTab] = useState<AvatarTab>("shape");
  const [uploadError, setUploadError] = useState("");
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLElement>(null);

  useEffect(() => { dialogRef.current?.focus(); }, []);
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  function moveTab(event: ReactKeyboardEvent<HTMLButtonElement>) {
    const index = tabs.findIndex(item => item.id === tab);
    if (event.key === "ArrowRight") setTab(tabs[(index + 1) % tabs.length].id);
    else if (event.key === "ArrowLeft") setTab(tabs[(index - 1 + tabs.length) % tabs.length].id);
    else return;
    event.preventDefault();
  }

  async function chooseFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setUploading(true);
    setUploadError("");
    try { await onUpload(file); }
    catch (error) { setUploadError(error instanceof Error && error.message.trim() ? error.message : "无法使用这张图片。"); }
    finally { setUploading(false); }
  }

  return <div className="puck-avatar-sheet-backdrop" role="presentation" onClick={onClose}>
    <section ref={dialogRef} className="puck-avatar-sheet" role="dialog" aria-modal="true" aria-labelledby="puck-avatar-sheet-title" tabIndex={-1} onClick={event => event.stopPropagation()}>
      <header className="puck-avatar-sheet-header">
        <button className="puck-avatar-sheet-back" type="button" aria-label="返回" onClick={onClose}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M12.5 4.5 7 10l5.5 5.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/></svg></button>
        <h2 id="puck-avatar-sheet-title">头像</h2>
        <button className="puck-avatar-sheet-done" type="button" onClick={onClose}>完成</button>
      </header>
      <div className="puck-avatar-sheet-tabs" role="tablist" aria-label="头像来源">
        {tabs.map(item => <button type="button" role="tab" id={`puck-avatar-tab-${item.id}`} key={item.id} aria-selected={tab === item.id} aria-controls={`puck-avatar-panel-${item.id}`} tabIndex={tab === item.id ? 0 : -1} onClick={() => setTab(item.id)} onKeyDown={moveTab}>{item.label}</button>)}
      </div>
      <div className="puck-avatar-sheet-preview"><PuckAvatar assistantId={assistantId} name={name} design={design} size="large" animated={false}/></div>
      {tab === "shape" && <div className="puck-avatar-sheet-card" id="puck-avatar-panel-shape" role="tabpanel" aria-labelledby="puck-avatar-tab-shape">
        <div className="puck-shape-options" role="radiogroup" aria-label="头像形状">{shapeRows.map(row => <div className="puck-shape-row" data-count={row.length} key={row[0].value}>{row.map(option => {
          const checked = design.shape === option.value;
          return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-shape-option${checked ? " selected" : ""}`} key={option.value} onClick={() => onShape(option.value)} onKeyDown={event => moveRadio(event, avatarShapes.map(item => item.value), design.shape, value => onShape(value as AvatarShape))}><PuckAvatar assistantId={`${assistantId}-${option.value}`} name={option.label} design={{ shape: option.value, color: design.color, colorHex: design.colorHex }} size="small" animated={false}/></button>;
        })}</div>)}</div>
        <div className="puck-color-options" role="radiogroup" aria-label="头像颜色">{colorRows.map(row => <div className="puck-color-row" data-count={row.length} key={row[0].value}>{row.map(option => {
          const checked = design.color === option.value;
          return <button type="button" role="radio" aria-label={option.label} aria-checked={checked} tabIndex={checked ? 0 : -1} data-avatar-choice={option.value} className={`puck-color-option${checked ? " selected" : ""}`} style={{ "--puck-choice-color": option.hex } as CSSProperties} key={option.value} onClick={() => onColor(option.value)} onKeyDown={event => moveRadio(event, avatarColors.map(item => item.value), design.color, value => onColor(value as AvatarColor))}><span aria-hidden="true"/></button>;
        })}</div>)}</div>
        <button className="puck-avatar-reset" type="button" onClick={onReset}>重置为默认</button>
      </div>}
      {tab === "generate" && <div className="puck-avatar-sheet-card puck-avatar-sheet-note" id="puck-avatar-panel-generate" role="tabpanel" aria-labelledby="puck-avatar-tab-generate">
        <p>头像生成还没有接到 Puck。这里不会发起生成请求。</p>
      </div>}
      {tab === "upload" && <div className="puck-avatar-sheet-card puck-avatar-sheet-note" id="puck-avatar-panel-upload" role="tabpanel" aria-labelledby="puck-avatar-tab-upload">
        <input ref={fileRef} className="puck-avatar-file" type="file" accept="image/png,image/jpeg,image/webp" aria-label="上传头像图片" onChange={event => void chooseFile(event)}/>
        <button className="puck-avatar-upload" type="button" disabled={uploading} onClick={() => fileRef.current?.click()}>{uploading ? "正在处理图片…" : "选择图片"}</button>
        {design.image && <button className="puck-avatar-clear-image" type="button" onClick={onClearImage}>移除照片</button>}
        <p>支持 PNG、JPEG 或 WebP。照片写入现有头像字段，并替换形状显示。超过保存上限的图片会先缩小。</p>
        {uploadError && <p className="puck-avatar-upload-error" role="alert">{uploadError}</p>}
      </div>}
    </section>
  </div>;
}
