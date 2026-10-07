import type { CSSProperties } from "react";

export type ArielLogoVariant = "auto" | "mythic" | "mono" | "micro";
export type ArielLogoTone = "auto" | "color" | "ink" | "white";

export interface ArielLogoProps {
  size?: number | string;
  variant?: ArielLogoVariant;
  tone?: ArielLogoTone;
  alt?: string;
  decorative?: boolean;
  className?: string;
}

export const arielLogoAssets = {
  color: "/brand/ariel-logo-mythic-color.png",
  ink: "/brand/ariel-logo-mythic-ink.png",
  white: "/brand/ariel-logo-mythic-white.png",
  micro: "/brand/ariel-logo-micro.svg",
} as const;

function pixelSize(size: number | string): number | null {
  if (typeof size === "number") return size;
  const match = size.trim().match(/^(\d+(?:\.\d+)?)px$/);
  return match ? Number(match[1]) : null;
}

export function resolveArielLogoVariant(size: number | string, variant: ArielLogoVariant): Exclude<ArielLogoVariant, "auto"> {
  if (variant !== "auto") return variant;
  const pixels = pixelSize(size);
  if (pixels === null || pixels < 128) return "micro";
  return pixels >= 320 ? "mythic" : "mono";
}

function cssSize(size: number | string): string {
  return typeof size === "number" ? `${size}px` : size;
}

function classNames(variant: Exclude<ArielLogoVariant, "auto">, className?: string): string {
  return ["ariel-logo", `ariel-logo--${variant}`, className].filter(Boolean).join(" ");
}

export function ArielLogo({ size = 32, variant = "auto", tone = "auto", alt, decorative = false, className }: ArielLogoProps) {
  const resolvedVariant = resolveArielLogoVariant(size, variant);
  const style = { width: cssSize(size) } as CSSProperties;
  const accessibility = decorative
    ? { "aria-hidden": true as const }
    : { role: "img", "aria-label": alt || "Ariel" };

  if (resolvedVariant === "micro") {
    return <span {...accessibility} className={classNames("micro", className)} data-variant="micro" data-tone={tone} style={style} />;
  }

  let resolvedTone: ArielLogoTone;
  let src: string;
  if (resolvedVariant === "mythic") {
    const mythicTone: "color" | "ink" | "white" = tone === "ink" || tone === "white" ? tone : "color";
    resolvedTone = mythicTone;
    src = arielLogoAssets[mythicTone];
  } else {
    resolvedTone = tone === "ink" || tone === "white" ? tone : "auto";
    src = resolvedTone === "white" ? arielLogoAssets.white : arielLogoAssets.ink;
  }

  return <picture {...accessibility} className={classNames(resolvedVariant, className)} data-variant={resolvedVariant} data-tone={resolvedTone} style={style}>
    {resolvedVariant === "mono" && resolvedTone === "auto" && <source media="(prefers-color-scheme: dark)" srcSet={arielLogoAssets.white} />}
    <img src={src} alt="" aria-hidden="true" />
  </picture>;
}
