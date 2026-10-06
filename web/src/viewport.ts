type NavigatorHints = Pick<Navigator, "userAgent" | "platform" | "maxTouchPoints">;

function isIOS(navigator: NavigatorHints): boolean {
  return /iPad|iPhone|iPod/.test(navigator.userAgent)
    || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
}

export function configureIOSInputViewport(document: Document, navigator: NavigatorHints): boolean {
  if (!isIOS(navigator)) return false;
  const viewport = document.querySelector<HTMLMetaElement>('meta[name="viewport"]');
  if (!viewport) return false;
  const tokens = viewport.content
    .split(",")
    .map(token => token.trim())
    .filter(token => token && !/^maximum-scale\s*=/i.test(token));
  viewport.content = [...tokens, "maximum-scale=1"].join(", ");
  return true;
}
