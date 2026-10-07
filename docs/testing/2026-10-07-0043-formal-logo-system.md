# 0043 正式 Logo 系统验收记录

- 日期：2026-10-07
- 规格：[0043 正式风暴鹰身 Logo 系统](../specs/0043-formal-logo-system.md)
- 结论：仓库资产、组件选择规则、产品替换、文档引用与 Chromium 视觉基线通过；物理 iPhone/Safari 待用户复验。

## 资产完整性

| 仓库资产 | SHA-256 | 像素/格式 |
| --- | --- | --- |
| `web/public/brand/ariel-logo-mythic-color.png` | `1d0b1387b665853ae23f920a1d3cf53488e32189e9a86e4a232844fd70f88d33` | 1254×1254 RGB PNG |
| `web/public/brand/ariel-logo-mythic-ink.png` | `f79df791d9e0e7b18f638747afcfdf4106bb12f2dbfdd5331d60c6306111079d` | 1254×1254 RGBA PNG |
| `web/public/brand/ariel-logo-mythic-white.png` | `93e800f7c16d982aa4c7a9d8926354fa6b681e073a30682aa60ba1a9e5671ab9` | 1254×1254 RGBA PNG |
| `web/public/brand/ariel-logo-micro.svg` | `658c049b3a31140caf0388cf8b3a4d1ee1879956344190814b05a202237eebdc` | SVG，`viewBox="0 0 64 64"` |

测试读取 PNG IHDR、解析 SVG，并校验四个 SHA-256 与批准源文件一致。`npm run build` 后四个文件由 Vite 原样复制到 `web/dist/brand/`。

## 产品与文档

- `ArielLogo` 覆盖 16–127px 微标、128–319px 单色神话版和 ≥320px 彩色神话主版，支持显式 variant/tone 与装饰性可访问语义。
- Night 登录页使用 128–160px 反白神话版；顶部/侧栏、加载态和 favicon 使用微标；README 首图使用 480px 彩色主版。
- 产品源码不再包含旧八向星形路径；微标使用正式 SVG mask 和 `currentColor`，未使用 CSS filter。
- `docs/brand/logo-guideline.html` 与 `docs/brand/brand-spec.md` 只引用仓库相对路径，所有图片/下载链接均可解析到四个正式资产。

## 自动与视觉验收

- `npm test`：19 个文件、132 个测试通过。
- `npm run test:ui`：5 个真实 Chromium 测试通过。
- `npm run build`：production build 通过。
- `GO111MODULE=on go test ./...`：通过。
- `web/e2e/brand-system.spec.ts-snapshots/desktop-brand-login-darwin.png`：桌面登录页 160px 反白神话版与 29px 微标。
- `web/e2e/mobile-session-navigation.spec.ts-snapshots/mobile-pinned-sidebar-darwin.png`：390×844 侧栏中性灰微标。
- `web/e2e/mobile-session-navigation.spec.ts-snapshots/mobile-session-loading-blue-darwin.png`：390×844 加载态蓝色微标。
- 以 1440×900 Chromium 直接打开 `docs/brand/logo-guideline.html`，标题与首屏视觉正常，16 个图片节点全部完成加载且自然尺寸非零。

Chromium 结果只能证明真实浏览器 viewport 下的布局与渲染，不等同于物理 iPhone/Safari 验收。
