const pairingCredentialPattern = /^p_[0-9a-f]{64}$/;

type PairingLocation = Pick<Location, "hash" | "pathname" | "search">;
type PairingHistory = Pick<History, "replaceState">;

export function isPairingCredential(value: string): boolean {
  return pairingCredentialPattern.test(value);
}

export function takePairingCredential(location: PairingLocation, history: PairingHistory): string {
  if (!location.hash.startsWith("#pair=")) return "";
  const value = location.hash.slice("#pair=".length);
  history.replaceState(null, "", `${location.pathname}${location.search}`);
  return isPairingCredential(value) ? value : "";
}

export function pairingURL(origin: string, credential: string): string {
  if (!isPairingCredential(credential)) throw new Error("invalid pairing credential");
  return `${origin.replace(/\/$/, "")}/#pair=${credential}`;
}

export async function pairingQRCode(url: string): Promise<string> {
  const QRCode = (await import("qrcode")).default;
  const svg = await QRCode.toString(url, {
    type: "svg",
    width: 280,
    margin: 2,
    errorCorrectionLevel: "M",
    color: { dark: "#000000ff", light: "#ffffffff" },
  });
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}
