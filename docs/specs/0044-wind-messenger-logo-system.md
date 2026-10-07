# 0044：正式“风之信使”Logo 系统

- 状态：Implemented（资产完整性、136 个 Web 单测、7 个真实 Chromium 用例与截图、production build、Go race/vet 已验；物理 iPhone/Safari 待用户复验）。
- 范围：Ariel Web、README、favicon、Apple Touch Icon、Web App Manifest、社交元数据、品牌文档、可复用 React 组件、自动测试与真实浏览器视觉基线。
- 取代：0043 的旧版正式 Logo 系统，以及 0037/0041 对加载标识颜色与实现方式的要求。

## 品牌语义

Ariel 使用用户提供的“风之信使”正式资产：侧身、闭目、中性精灵展开双翼，胸前晨星是唯一 relay core，六条风带由同一 core 发往六个 Codex App 终点。产品只消费批准资产，不重绘、描摹、裁切、旋转、拉伸、滤镜换色、加底板、描边、阴影、发光或额外装饰。

## 仓库资产与映射

Vite 静态资源放在 `web/public/brand/`：

| 设计源文件 | 仓库文件 |
| --- | --- |
| `assets/ariel-wind-messenger-color-master.png` | `ariel-logo-wind-messenger-color.png` |
| `assets/ariel-wind-messenger-color-512.png` | `ariel-logo-wind-messenger-color-512.png` |
| `assets/ariel-wind-messenger-mono-ink.png` | `ariel-logo-wind-messenger-ink.png` |
| `assets/ariel-wind-messenger-mono-white.png` | `ariel-logo-wind-messenger-white.png` |
| `assets/ariel-wind-messenger-micro.svg` | `ariel-logo-wind-messenger-micro.svg` |
| `assets/ariel-wind-messenger-micro-white.svg` | `ariel-logo-wind-messenger-micro-white.svg` |
| `brand-spec.md` | `docs/brand/brand-spec.md` |
| `index.html` | `docs/brand/logo-guideline.html` |

禁止复制 `assets/archive/romantic-drafts/`、任何旧版 Logo 资产或设计目录中的其他派生文件。落库后，六个运行时资产必须和批准源文件保持 SHA-256 字节一致；规范页只使用仓库相对路径。

## 尺寸与背景契约

| 最终显示宽度 | variant | 资源 |
| --- | --- | --- |
| `≥320px` | `color` | 彩色主版 |
| `128–319px` | `mono` | 浅底 ink、深底 white |
| `16–127px` | `micro` | 浅底 ink SVG、深底 white SVG |

完整人物主标不得缩入导航、favicon 或紧凑状态区。所有资源保持 1:1 宽高比，渲染使用 `object-fit: contain`。产品当前为固定 Night 主题，导航、侧栏、加载态与登录页必须显式选 white；组件的 `tone="auto"` 仍应使用浏览器主题机制选择浅/深候选。

## 组件契约

```ts
type ArielLogoVariant = "auto" | "color" | "mono" | "micro";
type ArielLogoTone = "auto" | "ink" | "white";

interface ArielLogoProps {
  size: number | string;
  variant?: ArielLogoVariant;
  tone?: ArielLogoTone;
  alt?: string;
  decorative?: boolean;
  className?: string;
  priority?: boolean;
}
```

行为：

1. 数字 `size` 与 `variant="auto"` 按三档边界选图；小于 16 的数字尺寸无效。
2. CSS 字符串尺寸不能可靠换算，必须显式提供 `variant`，否则开发与测试立即报错。
3. `variant="color"` 总是使用彩色主版；`mono`/`micro` 的显式 tone 使用对应 ink/white 文件。
4. `tone="auto"` 用 `<picture>` 的 `prefers-color-scheme` 候选适配主题；浏览器每次只请求匹配的一个实际资源，不同时渲染两份 Logo。
5. `priority=true` 为实际 `<img>` 设置高优先级和 eager loading。
6. 旁边已有 “Ariel” 时调用方传 `decorative`；独立 Logo 默认可访问名称为 `Ariel`。

## 替换范围

1. 顶部导航、侧栏、加载态使用 white 微标；登录页使用 white 单色主版。
2. README 首图使用 480px 彩色主版。
3. favicon 使用浅底微标；Apple Touch Icon、Manifest 512 icon 与 Open Graph/Twitter 图片使用批准的 512 彩色 PNG。
4. `docs/brand/` 使用新版规范，并修正设计预览里未随本次交付提供的单色 512 引用，使其指向正式单色主版。
5. 删除旧运行时资产；产品、文档、测试与示例不再引用旧文件名或旧图形。
6. 仓库没有 About、设置、Storybook 或桌面 App 壳时不虚构入口，在验收记录中明确为“不适用”。

## 验收

1. TDD：先让新资源、接口、Manifest/元数据、调用点和旧引用守卫失败，再实现至 green。
2. 组件测试覆盖 `16/24/32/64/128/256/320/512px`、字符串尺寸拒绝、显式 variant/tone、priority 与可访问语义。
3. 资产测试覆盖 SHA-256、PNG 尺寸/透明通道、SVG XML/viewBox/六终点、README/文档路径与旧引用归零。
4. 真实 Chromium 覆盖浅色、深色、近黑、透明画布、README 风格背景，以及上述八个关键尺寸；断言实际资源、几何比例、加载完成和背景配色。
5. 更新桌面登录页与手机侧栏/加载态截图基线；另保存关键尺寸矩阵截图。
6. 运行 format（若仓库有脚本）、Web lint/typecheck（若有脚本）、Web 全量单测、Playwright、production build 与 Go 全量测试。
7. 不自动 push；保留用户已有的无关修改与 `graphify-out/`。
