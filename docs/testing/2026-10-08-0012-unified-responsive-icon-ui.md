# 0012 跨端统一紧凑布局与 icon 操作验收

## 范围

- 桌面端复用手机端紧凑 Header、覆盖式会话抽屉、权限入口和 composer。
- 纯功能按钮使用内联 SVG icon，文字说明移入 hover／focus tooltip，并保留 `aria-label`。
- 语义型主操作继续保留文字，避免 icon 化降低审批、登录、创建和异常恢复的可理解性。

## TDD 证据

### Red

在实现前新增或收紧以下契约，实际运行后按预期失败：

- `App.test.tsx`：队列引导／删除／更多、附件、停止、发送、菜单和断开按钮必须无可见文字、包含装饰性 SVG，并提供 `data-tooltip`。
- `theme.test.ts`：紧凑 Header、抽屉、权限入口、圆角 composer 和 icon tooltip 必须在桌面基础样式中生效，而不是只存在于 `max-width: 800px` 分支。
- `desktop-unified-ui.spec.ts`：1280×720 下会话区域必须占满视口宽度，Header 为 66px，textarea 从 44px 起步，桌面侧栏使用覆盖式抽屉，并能显示发送 tooltip。

### Green

- 相关 Vitest：78 项通过。
- Web 全量 Vitest：20 个文件、151 项通过。
- Playwright：14 项通过，包含新增桌面端 fixture 与全部既有移动端行为／截图回归。
- `npm run build`：通过；保留既有单 chunk 超过 500 kB 的 Vite 提示。
- `go test ./...`：通过。
- `go vet ./...`：通过。

## 浏览器视觉与几何

### 1280×720 桌面端

- 已连接 masthead 隐藏；会话主区域 `x = 0`、宽度 `1280px`。
- Header 高度 `66px`；权限入口可见，常驻权限文字条隐藏。
- textarea 字号 `14px`、初始高度 `44px`；composer 圆角 `27px`。
- 队列引导、附件、停止和发送均为 icon-only；发送按钮 hover 后 tooltip opacity 为 `1`。
- 会话抽屉打开时覆盖主区域，遮罩可点击关闭，不改变对话区固有宽度。

截图：

- [桌面紧凑主界面](../../web/e2e/desktop-unified-ui.spec.ts-snapshots/desktop-unified-compact-ui-darwin.png)
- [桌面会话抽屉](../../web/e2e/desktop-unified-ui.spec.ts-snapshots/desktop-unified-sidebar-darwin.png)

### 390×844 移动端

重新运行并人工检查了登录、会话 Header、滚动收放、队列菜单／拖拽、置顶侧栏、Markdown 表格与会话图片截图。icon 替换后未出现横向溢出、按钮遮挡、菜单层级退化或输入区高度回退。

## 保留项

- 物理 iPhone/Safari 与不同桌面浏览器仍由用户按真实设备复验。
- tooltip 只在支持 hover 的精细指针设备出现；触控端依靠常见 icon、布局上下文和可访问名称，不模拟悬停状态。
