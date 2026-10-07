# 0043：正式风暴鹰身 Logo 系统

- 状态：Implemented（资产字节/尺寸/透明通道、组件与替换、132 个 Web 测试、5 个真实 Chromium 截图/行为测试、production build 与 Go 全量测试已验；物理 iPhone/Safari 待用户复验）。
- 范围：Ariel Web、favicon、README、品牌文档、可复用 React 组件、相关自动测试与真实浏览器视觉基线。

## 品牌语义

Ariel 的正式标识来自《暴风雨》中的空气精灵，以“风暴鹰身”表达同一个 Ariel 从中央接收命令，并把消息传递到多个位置。产品只能使用用户给定的正式资产，不重绘、不描摹、不裁切、不拉伸、不滤镜换色，也不保留旧八向星形标识混用。

## 仓库资产

Vite Web 的静态资源统一放在 `web/public/brand/`：

- `ariel-logo-mythic-color.png`：彩色神话主版；
- `ariel-logo-mythic-ink.png`：浅色背景深墨神话版；
- `ariel-logo-mythic-white.png`：深色背景反白神话版；
- `ariel-logo-micro.svg`：16–127px 单色微标。

品牌规范放在 `docs/brand/`。规范中的图片和下载链接必须改为仓库相对路径，只指向以上四个正式资产；不得包含 Open Design 本机绝对路径或已废弃资产。

## 组件契约

Web 提供可复用的 `ArielLogo`：

```ts
type ArielLogoVariant = "auto" | "mythic" | "mono" | "micro";
type ArielLogoTone = "auto" | "color" | "ink" | "white";

interface ArielLogoProps {
  size?: number | string;
  variant?: ArielLogoVariant;
  tone?: ArielLogoTone;
  alt?: string;
  decorative?: boolean;
  className?: string;
}
```

行为：

1. `variant="auto"` 对可判定的像素尺寸按三档选择：`≥320px` 使用彩色神话主版，`128–319px` 使用单色神话版，`16–127px` 使用微标。无法可靠换算的 CSS 尺寸默认使用微标，调用方可显式指定 variant。
2. `variant="mythic"` 的 `auto/color` 使用彩色主版；显式 `ink/white` 使用对应单色神话资产。
3. `variant="mono"` 的 `ink/white` 使用对应资产；`tone="auto"` 按系统明暗偏好选择，但固定 Night 产品界面必须显式使用 `white`。
4. `variant="micro"` 使用正式 SVG 作为 CSS mask，以 `currentColor` 适配产品主题；不得用 `filter` 或生成另一套彩色微标。
5. 所有档位保持 1:1 比例和 `contain`，size 表示显示宽度；不得裁切、拉伸、加底板、阴影、描边或发光。
6. `decorative=true` 时不进入可访问名称；否则使用 `alt`，未提供时默认名称为 `Ariel`。

## 产品替换

1. 顶部品牌栏、侧栏品牌栏和会话加载占位全部改用正式微标，移除旧内联八向星形 SVG。
2. favicon 改用正式微标。
3. README 首图使用不小于 320px 的彩色神话主版，并链接品牌使用规范与工程化规范。
4. 历史 spec 中关于旧 Logo 图形的要求保留审计语义，但必须注明已由本规格取代。

## 完整性与安全

- 四个正式资产的仓库副本必须与源文件 SHA-256 一致。
- PNG 尺寸保持 1254×1254；深墨与反白版保留 RGBA，彩色主版保持原始 RGB 画布。
- SVG 必须是可解析的 64×64 `viewBox`，产品代码仅引用 `/brand/...`。
- 品牌文档不得引入缺失的 128/512 派生资产；预览尺寸由 HTML/CSS 控制。

## 验收

1. 先以缺失组件、资产和新选择规则运行测试到 red，再复制资产并实现到 green。
2. 组件测试覆盖三档自动选择、显式 variant/tone、装饰与非装饰可访问语义。
3. 资产测试覆盖 SHA-256、PNG IHDR 尺寸/色彩类型、SVG 可解析性、README 与文档引用完整性。
4. App／主题测试确认旧内联标识已移除，产品微标通过 `currentColor` 呈现且无 CSS filter。
5. 真实 Chromium 以桌面登录页和 390×844 手机已连接/加载状态完成截图、尺寸、比例和颜色断言；物理 iPhone/Safari 仍由用户最终复验。
6. Web 全量测试、UI 测试、production build 与 Go 全量测试通过。
