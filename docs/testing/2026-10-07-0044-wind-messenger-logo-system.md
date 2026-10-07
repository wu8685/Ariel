# 0044 “风之信使”正式 Logo 系统验收记录

- 日期：2026-10-07
- 规格：[0044 “风之信使”正式 Logo 系统](../specs/0044-wind-messenger-logo-system.md)
- 结论：仓库资产、组件选择、产品替换、README、图标元数据、品牌文档、production build 与 Chromium 视觉基线通过；物理 iPhone/Safari 待用户复验。

## 资产映射与完整性

| 设计源文件 | 仓库文件 | SHA-256 | 像素/格式 |
| --- | --- | --- | --- |
| `assets/ariel-wind-messenger-color-master.png` | `web/public/brand/ariel-logo-wind-messenger-color.png` | `691d4beab26ab10684353ac81f2ab61827c1c72b0c264668a7f82cd591306d30` | 1254×1254 RGBA PNG |
| `assets/ariel-wind-messenger-color-512.png` | `web/public/brand/ariel-logo-wind-messenger-color-512.png` | `faa20f8e37fa45905d0a9ec8a923b2edbebcf8a2ae507802470f8ca26c303e6d` | 512×512 RGBA PNG |
| `assets/ariel-wind-messenger-mono-ink.png` | `web/public/brand/ariel-logo-wind-messenger-ink.png` | `308a248ea542b8d7bd0a76e4aa2d82ffd5307b0263a7f00cd73b28238460df91` | 1254×1254 RGBA PNG |
| `assets/ariel-wind-messenger-mono-white.png` | `web/public/brand/ariel-logo-wind-messenger-white.png` | `a85ec3b832dc15879dbff4a67ea444da7ee87e834ffcbcf0cb1a8eabd382c211` | 1254×1254 RGBA PNG |
| `assets/ariel-wind-messenger-micro.svg` | `web/public/brand/ariel-logo-wind-messenger-micro.svg` | `c87edfb3e3df51c91c94e50eaf5554ac9d104ba8dbbfe1e01792af776bdbd154` | SVG，64×64 viewBox |
| `assets/ariel-wind-messenger-micro-white.svg` | `web/public/brand/ariel-logo-wind-messenger-micro-white.svg` | `5de64a4a0b6b871e5a0d2839c8d1b4efed7a18eb2c7dee29c4f6618209b14f99` | SVG，64×64 viewBox |
| `brand-spec.md` | `docs/brand/brand-spec.md` | 源内容落库后适配仓库文件名 | Markdown |
| `index.html` | `docs/brand/logo-guideline.html` | 源内容落库后适配仓库相对路径 | HTML |

四张 PNG 的 IHDR 均为 8-bit RGBA；源文件 alpha 统计同时确认存在透明、半透明与不透明像素，四角透明。两张 SVG 均可解析，包含一个中央 core 图形和六个 receiver circles，无 filter/mask。六份运行时资产与批准源文件字节一致；未复制 archive 或清单之外的设计文件。

## 产品与组件

- `ArielLogo` 接口使用必填 `size`、`auto|color|mono|micro`、`auto|ink|white`、可访问文本、`decorative`、`className` 与 `priority`。
- 数字 size 在 128/320px 边界自动切档；CSS 字符串必须显式给 variant；小于 16px 的数字尺寸立即报错。
- `tone="auto"` 通过 `<picture>` 与 `prefers-color-scheme` 选择候选，DOM 中只有一个 `<img>`；固定 Night 产品调用显式选 white。
- 图片保持正方形与 `object-fit: contain`；没有 CSS filter、mask、cover、阴影、描边或发光。
- 登录页使用 128–160px 反白人物主版；导航、侧栏和会话加载态使用反白微标；README 以 480px 使用彩色主版。
- favicon 使用 ink 微标；Apple Touch Icon、Manifest 512 icon、Open Graph 与 Twitter image 使用正式 512 彩色 PNG。未添加自定义底板。
- 仓库没有 About、设置、Storybook 或桌面 App 壳，因此这些替换项不适用。

当前应用是 Vite SPA，不存在 SSR 渲染路径。`<picture>` 的主题候选由浏览器在资源选择阶段决定，不依赖 hydration；产品固定 Night 调用也不会在刷新时先显示 ink。仓库当前没有运行时主题切换入口；若未来增加应用内主题覆盖，应把 tone 与该主题状态显式连接。

## 自动与视觉验收

- `npm test`：19 个文件、136 个测试通过。
- `npm run build`：TypeScript project build 与 Vite production build 通过；`dist/brand/` 只包含六份新资产，Manifest 已输出。
- `npm run test:ui`：7 个真实 Chromium 测试通过。
- `GO111MODULE=on go test -race ./...`：通过。
- `GO111MODULE=on go vet ./...`：通过。
- `git diff --check`：通过。
- 仓库未定义 format 或 Web lint 独立脚本；类型检查由 `npm run build` 中的 `tsc -b` 完成。

视觉证据：

- `web/e2e/brand-system.spec.ts-snapshots/desktop-brand-login-darwin.png`：近黑登录页的 160px 反白主版与 29px 反白微标。
- `web/e2e/brand-system.spec.ts-snapshots/wind-messenger-size-matrix-darwin.png`：16/24/32/64/128/256/320/512px，覆盖浅底、GitHub README 白底、深底、近黑与明暗分割透明画布。
- `web/e2e/mobile-session-navigation.spec.ts-snapshots/mobile-pinned-sidebar-darwin.png`：390×844 手机侧栏中的 29px 反白微标。
- `web/e2e/mobile-session-navigation.spec.ts-snapshots/mobile-session-loading-wind-messenger-darwin.png`：390×844 会话加载态的 50px 反白微标。
- `web/e2e/brand-guideline.spec.ts-snapshots/wind-messenger-guideline-darwin.png`：仓库品牌规范整页，所有图片自然尺寸非零。

人工查看确认 PNG 在浅色、深色与分割背景上透明合成正常，没有棋盘格、黑底或白底；浅底使用 ink、深底使用 white，16/32px favicon 级微标保持清晰。Chromium 结果不等同于物理 iPhone/Safari；GitHub README 与外部社交爬虫的线上渲染也需在发布后复验。
