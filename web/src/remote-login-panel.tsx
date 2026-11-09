import { useState } from "react";
import { CircleButton } from "./circular-button";

function CloseIcon() {
  return <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><path d="m6 6 12 12M18 6 6 18"/></svg>;
}

export type RemoteLoginDialogStatus = "loading" | "waiting" | "approved" | "rejected" | "cancelled" | "expired" | "error";

const remoteStatusCopy: Record<Exclude<RemoteLoginDialogStatus, "loading" | "waiting" | "error">, string> = {
  approved: "已批准",
  rejected: "已拒绝",
  cancelled: "已取消",
  expired: "已过期",
};

export function RemoteLoginDialog({ status, qr, code, url, seconds, message, onClose, onRegenerate }: {
  status: RemoteLoginDialogStatus;
  qr: string;
  code: string;
  url: string;
  seconds: number;
  message: string;
  onClose: () => void;
  onRegenerate: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const terminal = status === "approved" || status === "rejected" || status === "cancelled" || status === "expired";
  return <div className="pairing-backdrop" role="presentation">
    <section className="pairing-dialog" role="dialog" aria-modal="true" aria-labelledby="remote-login-title">
      <CircleButton className="pairing-close icon-button icon-control" type="button" aria-label="关闭扫码远程登录" data-tooltip="关闭" onClick={onClose}><CloseIcon /></CircleButton>
      <span className="eyebrow">REMOTE LOGIN</span>
      <h2 id="remote-login-title">扫码远程登录</h2>
      {status === "loading" && <div className="pairing-state" role="status">正在创建一次性登录请求…</div>}
      {status === "waiting" && <>
        <div className="pairing-qr"><img src={qr} alt="扫码远程登录二维码" /></div>
        <p>用已登录 Ariel 的手机扫描，或在能使用 Passkey 的设备上打开链接。</p>
        <div className="remote-login-code" aria-label={`核对码 ${code}`}>{code}</div>
        <strong className="pairing-countdown" role="status">等待扫码 · {seconds} 秒后失效</strong>
        <input className="remote-login-link" aria-label="远程登录链接" readOnly value={url} />
        <button className="secondary" type="button" onClick={() => { const write = navigator.clipboard?.writeText(url); void write?.then(() => setCopied(true)).catch(() => setCopied(false)); }}>{copied ? "已复制" : "复制链接"}</button>
        <small>二维码只包含一次性挑战编号，不包含会话凭据。对方必须先使用 Passkey，再明确批准。</small>
      </>}
      {terminal && <div className={`pairing-state ${status === "approved" ? "success" : ""}`} role="status"><strong>{remoteStatusCopy[status]}</strong><span>{status === "approved" ? "正在连接这台浏览器。" : "请重新生成后再试。"}</span></div>}
      {status === "error" && <div className="pairing-state" role="alert"><strong>{message || "无法创建远程登录，请重试。"}</strong></div>}
      <div className="pairing-actions">
        {(status === "expired" || status === "rejected" || status === "cancelled" || status === "error") && <button className="primary" type="button" onClick={onRegenerate}>重新生成</button>}
        <button className="secondary" type="button" onClick={onClose}>关闭</button>
      </div>
    </section>
  </div>;
}

export function RemoteApprovalDialog({ status, code, message, busy, onApprove, onReject, onClose }: {
  status: string;
  code: string;
  message: string;
  busy: boolean;
  onApprove: () => void;
  onReject: () => void;
  onClose: () => void;
}) {
  const pending = status === "pending";
  return <div className="pairing-backdrop" role="presentation">
    <section className="pairing-dialog" role="dialog" aria-modal="true" aria-labelledby="remote-approval-title">
      <span className="eyebrow">REMOTE LOGIN</span>
      <h2 id="remote-approval-title">批准远程登录</h2>
      {pending && <>
        <p>请与远程浏览器上的核对码比对。批准后，那台浏览器获得普通登录会话。</p>
        <div className="remote-login-code" aria-label={`核对码 ${code}`}>{code}</div>
        <small>Passkey 不会离开这台设备，二维码里也没有会话凭据。只点击批准才会继续。</small>
        {message && <p role="alert" className="connect-error">{message}</p>}
        <div className="pairing-actions">
          <button className="primary" type="button" onClick={onApprove} disabled={busy}>批准远程登录</button>
          <button className="secondary" type="button" onClick={onReject} disabled={busy}>拒绝</button>
        </div>
      </>}
      {!pending && <div className={`pairing-state ${status === "approved" ? "success" : ""}`} role="status">
        <strong>{status === "approved" ? "已批准" : status === "rejected" ? "已拒绝" : "无法批准"}</strong>
        <span>{message || (status === "approved" ? "远程浏览器正在登录。" : "这个登录请求已经结束。")}</span>
        <div className="pairing-actions"><button className="secondary" type="button" onClick={onClose}>完成</button></div>
      </div>}
    </section>
  </div>;
}
