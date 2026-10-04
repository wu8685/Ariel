const key = "ariel.web-session.v1";
const validSessionToken = /^s_[0-9a-f]{64}$/;

type SessionStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

export class WebSession {
  constructor(private readonly storage: () => SessionStorage) {}

  saved(): string {
    try {
      const token = this.storage().getItem(key) || "";
      return validSessionToken.test(token) ? token : "";
    } catch { return ""; }
  }

  accepted(token: string): void {
    if (!validSessionToken.test(token)) return;
    try { this.storage().setItem(key, token); } catch { /* Private browsing can disable storage. */ }
  }

  rejected(): void { this.clear(); }
  disconnect(): void { this.clear(); }

  private clear(): void {
    try { this.storage().removeItem(key); } catch { /* Connection can still be closed. */ }
  }
}
