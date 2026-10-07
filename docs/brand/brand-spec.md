---
name: Ariel visual system
source: https://github.com/wu8685/Ariel
extracted: 2026-10-07
---

# Ariel visual system

## Source observations

- Ariel is a private relay that lets a phone continue a Codex Desktop session on a Mac; the original session, working directory, and permissions remain local.
- The shipped product uses a Night interface: black page, near-black surfaces, white text, neutral-gray brand mark, and blue interaction highlights.
- The former in-product mark was an eight-spoke monoline asterisk. It has been fully replaced by the formal storm-harpy micro mark, rendered through `currentColor` in compact product surfaces.
- The product posture is compact, technical, private, and persistent rather than playful or cloud-like.

## Core tokens

```css
:root {
  --bg:      oklch(0% 0 0);               /* source #000000 */
  --surface: oklch(15.91% 0 0);           /* source #0d0d0d */
  --fg:      oklch(100% 0 0);             /* source #ffffff */
  --muted:   oklch(78.26% 0 0);           /* source #b8b8b8 */
  --border:  oklch(36.77% 0 0);           /* source #3f3f3f */
  --accent:  oklch(73.18% 0.1397 258.07); /* source #6fa9ff */

  --font-display: Georgia, 'Songti SC', serif;
  --font-body: Inter, 'SF Pro Text', 'PingFang SC', -apple-system, BlinkMacSystemFont, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
```

## Supporting product colors

- Primary action: `oklch(50.22% 0.1762 260.15)` from `#1d5dc6`.
- Connected/success: `oklch(79.39% 0.1307 163.92)` from `#5dd6a5`.
- Caution: `oklch(82.98% 0.0993 62.31)` from `#f6b983`.

## Adopted mythic mark palette — vivid color edition

| Token | HEX | OKLCH | Role |
| --- | --- | --- | --- |
| Night Canvas | `#02040A` | `oklch(10.71% 0.0194 262.03)` | Primary dark canvas and avatar background |
| Tempest Navy | `#071A3D` | `oklch(22.63% 0.0719 261.12)` | Outer silhouette and figure body |
| Wing Cobalt | `#0B4F9C` | `oklch(43.53% 0.1404 256.01)` | Wing depth and inner strokes |
| Storm Silver | `#F2FAFF` | `oklch(98.05% 0.0108 234.81)` | Face, feathers, and highlight planes |
| Relay Azure | `#00A8FF` | `oklch(70.26% 0.1696 242.92)` | Relay core, message routes, and endpoints only |
| Air Cyan | `#63F4FF` | `oklch(89.33% 0.1238 202.21)` | Small signal highlight only |
| Command Gold | `#FFC247` | `oklch(84.87% 0.1508 81.27)` | Command source, relay-core inner ring, storm-flash fill |
| Tempest Coral | `#FF5A47` | `oklch(68.57% 0.2038 29.77)` | Outline on the two storm flashes only |

The vivid edition uses value separation instead of indiscriminate saturation: deep navy holds the silhouette, cobalt models the wings, silver keeps the face readable, azure/cyan carries the outgoing message paths, and gold identifies the single command source. Relay Azure has a `7.85:1` contrast ratio against Night Canvas.

The full-color treatment belongs only to the mythic master. Do not derive a separate flattened or cartoon-like product-color mark from it. Product chrome may still use the supporting interface colors above, but the identity mark itself switches from the full mythic master directly to monochrome.

Monochrome contracts:

- Light background: Deep Ink `#111827` / `oklch(21.01% 0.0318 264.66)`.
- Dark background: Light Ink `#F7F9FC` / `oklch(98.14% 0.0045 258.32)`.
- The monochrome artwork is a direct simplification of the original grayscale mythic Ariel: retain the adult frontal face, enclosing harpy wings, storm points, relay core, and ten-route message structure.
- Remove grain, gradients, glow, material shading, doubled contours, decorative curls, and feather scratches. Use major silhouette planes, primary feather cuts, and three line-weight levels only.
- Monochrome variants must remain true one-ink artwork. Do not retain blue endpoints, introduce a second tone, or simulate depth with decorative shading.

## Size tiers

| Final displayed width | Asset | Use |
| --- | --- | --- |
| `≥ 320 px` | `web/public/brand/ariel-logo-mythic-color.png` | README hero, mythic hero art, launch visuals, social source artwork, presentation covers |
| `128–319 px` | `web/public/brand/ariel-logo-mythic-ink.png` or `web/public/brand/ariel-logo-mythic-white.png` | README medium display, website lockup, login, documents, print, monochrome campaigns |
| `16–127 px` | `web/public/brand/ariel-logo-micro.svg` | App navigation, favicon, status surfaces |

The color and simplified monochrome mythic marks share the same ten-route narrative structure. Only the micro mark reduces the story to six routes because a recognizable face and feather system cannot survive below 128 px.

- Absolute digital minimum: `16 × 16 px`, micro mark only.
- Simplified monochrome mythic minimum: `128 × 128 px` or `24 × 24 mm` in print.
- Mythic-master minimum: `320 × 320 px` or `60 × 60 mm` in print.
- Define `1×` as the relay core diameter. Keep at least `1×` clear space on all four sides.
- In a square avatar, the full mark should occupy 72–78% of the canvas. Never crop the top/bottom storm points or lateral endpoints.
- Mark-to-wordmark spacing: `0.75×`.

## Layout and mark posture

1. Keep the mythic face and enclosing harpy-wing silhouette identical across full-color and monochrome master marks; simplify line density instead of replacing the character with a geometric mascot.
2. Use the full-color mythic master only at narrative scale. Use one-ink Deep Ink or Light Ink at product and document scale; do not create a separate product-color logo.
3. Prefer radial or converging geometry to express many clients entering one relay; avoid generic cloud, Wi-Fi, chat-bubble, or hexagon imagery.
4. Use square or circular canvases with minimal rounding. Monochrome versions have no gradients, shadows, glow, texture, or decorative depth.
5. Pair the symbol with the existing serif `Ariel` wordmark in product contexts; use the symbol alone for avatars and favicons.

## Repository integration

- Product URLs use `/brand/ariel-logo-mythic-color.png`, `/brand/ariel-logo-mythic-ink.png`, `/brand/ariel-logo-mythic-white.png`, and `/brand/ariel-logo-micro.svg` from Vite's `web/public/brand/` root.
- Use the shared `web/src/ArielLogo.tsx` component instead of writing inline logo SVGs or directly selecting an asset in feature code.
- The README hero is the 480px color master; the Night login uses the 128–160px white mythic mark; navigation, loading and favicon use the micro mark.
- `docs/brand/logo-guideline.html` references the same repository assets through relative paths. It does not carry a second asset copy.
