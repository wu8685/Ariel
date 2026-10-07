---
name: Ariel visual system
source: https://github.com/wu8685/Ariel
adopted: 2026-10-07
direction: wind-messenger
---

# Ariel visual system — 风之信使

## Brand idea

Ariel 的名称来自莎士比亚《暴风雨》中的空气精灵。他替 Prospero 在多处传令，能够化作风、火焰与飞鸟。

正式 Logo 将这一典故转译为适合办公软件的「风之信使」：

- 侧身、闭目、中性精灵，表达温和与专注，不再正面凝视。
- 打开的双翼表达自由、持续运行与跨设备移动。
- 胸前晨星是唯一 relay core，也是唯一命令源。
- 六条风带从同一个 core 出发，通向六个接收点，表达多个 Codex App。
- 视觉气质是浪漫、可靠、清醒，不是卡通吉祥物，也不是暗黑游戏阵营。

## Canonical assets

| Asset | File | Final display width | Primary use |
| --- | --- | --- | --- |
| Full-color master | `web/public/brand/ariel-logo-wind-messenger-color.png` | `≥ 320 px` | README hero, website hero, launch art, social visual |
| Full-color compact PNG | `web/public/brand/ariel-logo-wind-messenger-color-512.png` | `≥ 320 px` | avatar source, social and documentation use |
| Deep-ink monochrome | `web/public/brand/ariel-logo-wind-messenger-ink.png` | `128–319 px` | light backgrounds, print, docs, lockups |
| Reverse monochrome | `web/public/brand/ariel-logo-wind-messenger-white.png` | `128–319 px` | dark product surfaces and presentations |
| Deep-ink micro mark | `web/public/brand/ariel-logo-wind-messenger-micro.svg` | `16–127 px` | favicon, navigation, compact status surfaces |
| Reverse micro mark | `web/public/brand/ariel-logo-wind-messenger-micro-white.svg` | `16–127 px` | dark navigation and compact dark surfaces |

Only the six repository assets listed above are approved for runtime use. Rejected or intermediate exploration files are not copied into this repository.

## Core tokens

HEX values are the asset-production anchors. OKLCH values are the CSS equivalents used by the preview and product surfaces.

```css
:root {
  --ariel-cloud:      #F7FBFF;
  --ariel-indigo:     #162B5C;
  --ariel-blue:       #246BFD;
  --ariel-sky:        #72C7FF;
  --ariel-iris:       #7A72E8;
  --ariel-gold:       #F6B942;

  --ariel-bg:         oklch(98.7% 0.007 245);
  --ariel-fg:         oklch(29.5% 0.083 263);
  --ariel-primary:    oklch(56% 0.23 259);
  --ariel-air:        oklch(80% 0.12 235);
  --ariel-secondary:  oklch(60% 0.15 285);
  --ariel-command:    oklch(81% 0.14 82);

  --font-display: 'Iowan Old Style', 'Songti SC', Georgia, serif;
  --font-body: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'PingFang SC', system-ui, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
```

### Color roles

| Color | Role | Constraint |
| --- | --- | --- |
| Cloud White `#F7FBFF` | feather light, light canvas, reverse mark | May occupy large surfaces |
| Wind Indigo `#162B5C` | silhouette, text, light-surface monochrome | Only monochrome master color on light backgrounds |
| Relay Blue `#246BFD` | primary product action, deep wing planes | Do not flood full product surfaces |
| Open Sky `#72C7FF` | air routes and light wing planes | Supporting color only |
| Iris Air `#7A72E8` | secondary wind route | Use once or twice per composition |
| Dawn Gold `#F6B942` | central command source and six receivers | Never use as general decoration |

## Size contracts

| Final displayed width | Required asset tier | Notes |
| --- | --- | --- |
| `≥ 320 px` | Full-color master | Full mythic story and color layers are legible |
| `128–319 px` | Monochrome master | Use ink on light surfaces, white on dark surfaces |
| `16–127 px` | Micro mark | Do not downscale the full character artwork |

- Absolute digital minimum: `16 × 16 px`, micro mark only.
- Common product navigation sizes: `24`, `28`, `32`, or `40 px`, micro mark only.
- Common app/avatar source: `512 × 512 px`, use the compact full-color PNG on a Wind Indigo background.
- Define `1×` as the diameter of the central command star. Keep at least `1×` clear space on all sides.
- In a square avatar, the mark should occupy `76%–82%` of the canvas. Do not crop wing tips, ribbons, or receiver stars.
- Mark-to-wordmark spacing: `0.75×`.

## Layout posture

1. Use an open, airy canvas. The mark should feel as if it is moving through morning air, not trapped in a badge.
2. Keep the figure in calm side profile. Do not add direct gaze, large eyes, smile, mask, claws, or weapons.
3. Preserve one central command star and exactly six outgoing routes in full and monochrome masters.
4. Keep gold limited to the command source and receiver stars. Do not add gold borders, gold wordmarks, or decorative star fields.
5. Use the serif `Ariel` wordmark beside the symbol in brand contexts; product UI body copy remains system sans.
6. Full-color artwork is an identity illustration, not a navigation icon. Switch tiers instead of sharpening or compressing it.

## Monochrome contract

- Light background: use Deep Ink `#162B5C` only.
- Dark background: use Cloud White `#F7FBFF` only.
- Both files are true transparent one-ink PNGs with identical geometry.
- Do not create monochrome variants with CSS `filter`, opacity tricks, gradient masks, or grayscale conversion at runtime.
- Do not add a colored endpoint, glow, shadow, second tone, or circular badge.

## Micro mark contract

The micro mark deliberately drops the human profile. At `16–127 px`, it keeps the three durable genes that survive:

1. two open wing gestures;
2. one central command star;
3. six routes terminating in six receivers.

Use the provided SVGs. Do not redraw the micro mark from the full-color PNG and do not use a Unicode star or asterisk substitute.

## Product usage map

| Surface | Asset |
| --- | --- |
| README header / website hero | `ariel-logo-wind-messenger-color.png` at `320–420 px` |
| GitHub organization avatar | `ariel-logo-wind-messenger-color-512.png` on `#162B5C` square canvas |
| Light About / login brand block | `ariel-logo-wind-messenger-ink.png` |
| Dark About / login brand block | `ariel-logo-wind-messenger-white.png` |
| Light product navigation | `ariel-logo-wind-messenger-micro.svg` |
| Dark product navigation | `ariel-logo-wind-messenger-micro-white.svg` |
| Favicon | `ariel-logo-wind-messenger-micro.svg` |
| Print / monochrome document | corresponding monochrome master, never the color PNG converted at runtime |

## Forbidden use

- Do not use retired predecessor assets in new product code.
- Do not import rejected or intermediate exploration files.
- Do not redesign the character into a cartoon mascot or dark fantasy emblem.
- Do not change the route count, crop the six receiver stars, or add extra decorative stars.
- Do not stretch, rotate, skew, or use `object-fit: cover`.
- Do not apply CSS hue rotation, saturation, grayscale, drop-shadow, neon glow, or outline.
- Do not place the full-color master inside a small navigation slot.
- Do not add a rounded-square or circular container unless the destination platform requires an app-icon background.

## Accessibility

- If the symbol is the only visible brand name, use `alt="Ariel"`.
- If the word `Ariel` is already adjacent, use `alt=""` or `aria-hidden="true"` for the symbol.
- Never include “image”, “icon”, or the full mythic description in ordinary UI alt text.
- Reserve the descriptive alt text for documentation or brand-guideline pages.
