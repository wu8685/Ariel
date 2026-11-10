import { avatarImage } from "./PuckAvatar";

const acceptedTypes = new Set(["image/png", "image/jpeg", "image/webp"]);

function readAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("无法读取这张图片。"));
    reader.onload = () => typeof reader.result === "string" ? resolve(reader.result) : reject(new Error("无法读取这张图片。"));
    reader.readAsDataURL(file);
  });
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image();
    const timer = window.setTimeout(() => reject(new Error("无法读取这张图片。")), 1500);
    const finish = (ok: boolean) => {
      window.clearTimeout(timer);
      if (ok && image.naturalWidth > 0) resolve(image);
      else reject(new Error("无法读取这张图片。"));
    };
    image.onload = () => finish(true);
    image.onerror = () => finish(false);
    image.src = src;
  });
}

// The stored avatar image is the existing puck field: https URL or a png/jpeg/webp
// data URI no longer than 262144 characters. Larger files are scaled down here.
export async function readAvatarImageFile(file: File): Promise<string> {
  if (!acceptedTypes.has(file.type)) throw new Error("仅支持 PNG、JPEG 或 WebP。");
  const original = await readAsDataURL(file);
  const direct = avatarImage(original);
  if (direct) return direct;
  const image = await loadImage(original);
  let side = 256;
  let quality = 0.82;
  for (let attempt = 0; attempt < 6; attempt += 1) {
    const canvas = document.createElement("canvas");
    const longest = Math.max(image.width, image.height) || 1;
    const scale = Math.min(1, side / longest);
    canvas.width = Math.max(1, Math.round(image.width * scale));
    canvas.height = Math.max(1, Math.round(image.height * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error("无法处理这张图片。");
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    const stored = avatarImage(canvas.toDataURL("image/jpeg", quality));
    if (stored) return stored;
    side = Math.round(side * 0.72);
    quality = Math.max(0.45, quality - 0.12);
  }
  throw new Error("图片仍然太大，无法保存为头像。");
}
