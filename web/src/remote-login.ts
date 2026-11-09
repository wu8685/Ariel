export const remoteLoginIDPattern = /^[A-Za-z0-9_-]{22}$/;

const remoteLoginErrorText: Record<string, string> = {
  "Passkey enrollment required": "还没有 Passkey，无法发起远程登录。",
  "remote login capacity exceeded": "远程登录请求过多，请稍后再试。",
  "remote login not found": "这个远程登录请求已失效。",
  "remote login expired": "这个远程登录请求已过期。",
  "remote login already decided": "这个远程登录请求已经处理过。",
  "authentication required": "需要先使用 Passkey 登录，才能批准。",
  "origin not allowed": "当前页面来源不被允许。",
};

export type RemoteLoginChallenge = { id: string; pollToken: string; expiresAt: string; code: string };
export type RemoteLoginStatus = "pending" | "approved" | "rejected" | "cancelled" | "expired";
export type RemoteLoginView = { status: RemoteLoginStatus; expiresAt?: string; code?: string };

type RemoteLocation = { pathname: string; search: string; hash: string };
type RemoteHistory = { replaceState: (data: unknown, unused: string, url?: string | URL | null) => void };

export function takeRemoteLoginID(location: RemoteLocation, history: RemoteHistory): string {
  const params = new URLSearchParams(location.search.startsWith("?") ? location.search.slice(1) : location.search);
  if (!params.has("remoteLogin")) return "";
  const value = params.get("remoteLogin") ?? "";
  params.delete("remoteLogin");
  const query = params.toString();
  history.replaceState(null, "", `${location.pathname}${query ? `?${query}` : ""}${location.hash}`);
  return remoteLoginIDPattern.test(value) ? value : "";
}

export function remoteLoginURL(origin: string, id: string): string {
  if (!remoteLoginIDPattern.test(id)) throw new Error("invalid remote login");
  return `${origin.replace(/\/$/, "")}/?remoteLogin=${id}`;
}

async function requestJSON<T>(url: string, body: unknown): Promise<T> {
  const response = await fetch(url, {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await response.json().catch(() => ({})) as { error?: string } & T;
  if (!response.ok) throw new Error(remoteLoginErrorText[payload.error ?? ""] || payload.error || `远程登录请求失败（${response.status}）`);
  return payload;
}

export function startRemoteLogin(): Promise<RemoteLoginChallenge> {
  return requestJSON("/api/auth/remote-login/start", {});
}

export function readRemoteLogin(id: string): Promise<RemoteLoginView> {
  return requestJSON("/api/auth/remote-login/status", { id });
}

export function approveRemoteLogin(id: string): Promise<RemoteLoginView> {
  return requestJSON("/api/auth/remote-login/approve", { id });
}

export function rejectRemoteLogin(id: string): Promise<RemoteLoginView> {
  return requestJSON("/api/auth/remote-login/reject", { id });
}

export function pollRemoteLogin(id: string, pollToken: string): Promise<RemoteLoginView> {
  return requestJSON("/api/auth/remote-login/poll", { id, pollToken });
}

export function cancelRemoteLogin(id: string, pollToken: string): Promise<RemoteLoginView> {
  return requestJSON("/api/auth/remote-login/cancel", { id, pollToken });
}
