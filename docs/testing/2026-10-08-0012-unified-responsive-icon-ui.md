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

- 相关 Vitest：82 项通过。
- Web 全量 Vitest：20 个文件、155 项通过。
- Playwright：14 项通过，包含新增桌面端 fixture 与全部既有移动端行为／截图回归。
- `npm run build`：通过；保留既有单 chunk 超过 500 kB 的 Vite 提示。
- `go test ./...`：通过。
- `go vet ./...`：通过。

## 浏览器视觉与几何

### 1280×720 桌面端

- 已连接 masthead 隐藏；会话主区域 `x = 0`、宽度 `1280px`。
- Header 高度 `66px`；权限入口可见，常驻权限文字条隐藏。
- textarea 字号 `14px`、初始高度 `44px`；composer 与排队 work item 圆角均为 `16px`。
- 队列引导、附件、停止和发送均为 icon-only；发送按钮 hover 后 tooltip opacity 为 `1`。
- 会话抽屉打开时覆盖主区域，遮罩可点击关闭，不改变对话区固有宽度。

截图：

- [桌面紧凑主界面](../../web/e2e/desktop-unified-ui.spec.ts-snapshots/desktop-unified-compact-ui-darwin.png)
- [桌面会话抽屉](../../web/e2e/desktop-unified-ui.spec.ts-snapshots/desktop-unified-sidebar-darwin.png)

### 390×844 移动端

重新运行并人工检查了登录、会话 Header、滚动收放、队列菜单／拖拽、置顶侧栏、Markdown 表格与会话图片截图。icon 替换后未出现横向溢出、按钮遮挡、菜单层级退化或输入区高度回退。

## 圆角一致性补充验收

- Red：样式契约与桌面／移动 Playwright 先要求 composer 和 queue panel 均为 `16px`，旧实现因 composer `27px`、桌面 queue panel `12px` 而失败。
- Green：新增 `--radius-work-item: 16px`；queue panel 与 composer 在基础样式和移动断点中均只引用该 token。
- 1280×720 桌面 Chromium 与 390×844 移动 Chromium 的计算样式断言均通过；队列菜单、拖拽、历史收放、Markdown 表格、会话图片和加载状态截图基线同步更新并完成全量复跑。
- 人工复核桌面紧凑主界面和移动队列菜单截图，输入框四角与排队 work item 外框弧度一致，没有新增溢出或遮挡。

## 会话侧栏紧凑化补充验收

- Red：DOM 契约因不存在 `.sidebar-toolbar`、仍保留独立 `.sidebar-head`／`.device-label`／`.device-meta` 而失败；样式契约因顶部仍为多段大间距布局而失败。浏览器几何断言先于实现加入，旧截图基线准确暴露了布局变化。
- Green：品牌、Relay 状态、新建、扫码、刷新、断开与关闭合并为单行工具栏；设备选择和 readiness 合并为 36px 单行；搜索收紧为第三行，统计下沉到紧邻列表的轻量标题。低频状态文字改为状态点和 `aria-label`，功能与无障碍名称均保留。
- 1280×720 桌面与 390×844 移动 Chromium 均确认 `.thread-list` 顶部不超过 `190px`，相比原约 340px 的顶部堆叠显著增加可见会话空间；设备离线竞态、扫码、刷新、新建、断开和关闭的既有测试全部通过。
- [桌面侧栏截图](../../web/e2e/desktop-unified-ui.spec.ts-snapshots/desktop-unified-sidebar-darwin.png)
- [移动侧栏截图](../../web/e2e/mobile-session-navigation.spec.ts-snapshots/mobile-pinned-sidebar-darwin.png)

## 会话 item 信息密度补充验收

- Red：样式契约因 `.thread-row` 仍为 `11px 14px`、手机断点仍覆盖为 `15px` padding 而失败；真实浏览器测得普通会话 item 高度为桌面 `65px`、手机 `73px`，均超过 `52px` 上限。
- Green：基础样式统一为 `7px 12px` padding、`1px 0` margin；标题为 `13px`，时间为 `9px`，标题／时间／搜索摘要间距同步收紧；移除手机端宽松 padding 覆盖。
- 桌面选中会话与手机置顶会话均通过 `≤52px` 几何断言；项目分组按钮未修改。两端截图确认标题和时间未裁切，选中边框、置顶区和项目分隔仍清晰。

## 保留项

- 物理 iPhone/Safari 与不同桌面浏览器仍由用户按真实设备复验。
- tooltip 只在支持 hover 的精细指针设备出现；触控端依靠常见 icon、布局上下文和可访问名称，不模拟悬停状态。
