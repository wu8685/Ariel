import type { CSSProperties } from "react";

export type ArielLogoVariant = "auto" | "color" | "mono" | "micro";
export type ArielLogoTone = "auto" | "ink" | "white";

export interface ArielLogoProps {
  size: number | string;
  variant?: ArielLogoVariant;
  tone?: ArielLogoTone;
  alt?: string;
  decorative?: boolean;
  className?: string;
  priority?: boolean;
}

export const arielLogoAssets = {
  color: "/brand/ariel-logo-wind-messenger-color.png",
  color512: "/brand/ariel-logo-wind-messenger-color-512.png",
  ink: "/brand/ariel-logo-wind-messenger-ink.png",
  white: "/brand/ariel-logo-wind-messenger-white.png",
  microInk: "/brand/ariel-logo-wind-messenger-micro.svg",
  microWhite: "/brand/ariel-logo-wind-messenger-micro-white.svg",
} as const;

export function resolveArielLogoVariant(size: number | string, variant: ArielLogoVariant): Exclude<ArielLogoVariant, "auto"> {
  if (typeof size === "number" && (!Number.isFinite(size) || size < 16)) {
    throw new Error("ArielLogo numeric size must be at least 16px.");
  }
  if (variant !== "auto") return variant;
  if (typeof size !== "number") {
    throw new Error("ArielLogo requires an explicit variant when size is a CSS string.");
  }
  if (size < 128) return "micro";
  return size < 320 ? "mono" : "color";
}

function cssSize(size: number | string): string {
  return typeof size === "number" ? `${size}px` : size;
}

function classNames(variant: Exclude<ArielLogoVariant, "auto">, className?: string): string {
  return ["ariel-logo", `ariel-logo--${variant}`, className].filter(Boolean).join(" ");
}

function sources(variant: Exclude<ArielLogoVariant, "auto">): { light: string; dark: string } {
  if (variant === "micro") return { light: arielLogoAssets.microInk, dark: arielLogoAssets.microWhite };
  if (variant === "mono") return { light: arielLogoAssets.ink, dark: arielLogoAssets.white };
  return { light: arielLogoAssets.color, dark: arielLogoAssets.color };
}

export function ArielLogo({
  size,
  variant = "auto",
  tone = "auto",
  alt,
  decorative = false,
  className,
  priority = false,
}: ArielLogoProps) {
  const resolvedVariant = resolveArielLogoVariant(size, variant);
  const candidates = sources(resolvedVariant);
  const autoTone = tone === "auto" && resolvedVariant !== "color";
  const resolvedTone = resolvedVariant === "color" ? "color" : tone;
  const src = resolvedVariant === "color" ? candidates.light : tone === "white" ? candidates.dark : candidates.light;
  const dimension = cssSize(size);
  const style = { width: dimension, height: dimension } as CSSProperties;
  const accessibility = decorative
    ? { "aria-hidden": true as const }
    : { role: "img", "aria-label": alt || "Ariel" };

  return <picture
    {...accessibility}
    className={classNames(resolvedVariant, className)}
    data-variant={resolvedVariant}
    data-tone={resolvedTone}
    style={style}
  >
    {autoTone && <source media="(prefers-color-scheme: dark)" srcSet={candidates.dark} />}
    <img
      src={src}
      alt=""
      aria-hidden="true"
      decoding="async"
      loading={priority ? "eager" : undefined}
      fetchPriority={priority ? "high" : undefined}
    />
  </picture>;
}
