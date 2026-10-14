# 0082 项目级本地 unread 标记验收

- 日期：2026-10-14
- 规格：[`0082-project-level-local-unread.md`](../specs/0082-project-level-local-unread.md)
- 结论：实现、自动测试、真实移动 Chromium 与公网 Docker 发布均通过；物理 iPhone／Safari 待用户复验。

## 行为与数据边界

项目 unread 由 Web 已有的会话列表和 `unreadThreadIDs` 纯派生：当前项目只要存在一个 unread 会话，项目标题便展示同款蓝点。收起或展开项目不会改变已读水位；逐个查看会话时，直到最后一个 unread 被查看，项目蓝点才消失。

该实现没有新增 Relay／Desktop Agent／Codex API 请求，也没有建立项目级持久化水位。置顶会话仍按其 `cwd` 计入已有项目，搜索时只聚合当前搜索结果。

## TDD 证据

Red 阶段先增加项目聚合纯函数测试以及 App 交互测试。旧实现分别因缺少导出函数和页面标记而失败：

```text
TypeError: unreadProjectKeys is not a function
Unable to find an element with the role "status" and name "项目有新结果"
```

Green 阶段增加 `unreadProjectKeys` 纯派生函数和项目标题蓝点。测试覆盖：

- 无 unread、单项目、多项目、规范化路径与置顶会话聚合；
- 折叠项目显示蓝点，展开不清除；
- 查看一个 unread 后项目仍保留，查看最后一个后消失；
- 整个交互过程中 `thread.list` 请求次数保持原值，没有逐项目或逐会话请求。

## 自动验证

```text
Vitest: 24 files, 197 tests passed
Playwright: 29 tests passed
Vite production build: passed
```

固定手机 viewport 的真实 Chromium 验证了折叠和展开状态、项目名称、蓝点与右侧会话数量之间的几何关系。截图基线：

- `web/e2e/mobile-thread-unread.spec.ts-snapshots/mobile-project-unread-dot-darwin.png`
- `web/e2e/mobile-thread-unread.spec.ts-snapshots/mobile-local-unread-dot-darwin.png`

## 公网发布验证

公网 `www.kan.chat` 已部署 `ariel-relay:220cb24`。验证结果：

```text
container: ariel-relay:220cb24 status=running health=healthy
healthz: HTTP 200
web assets: index-BP4MVgQY.js, index-BtZ1psSD.css
Desktop Agent -> Relay: ESTABLISHED
```

旧容器保留为已停止的 `ariel-relay-prev-c226db2`，需要时可恢复。

## 保留复验

- 在物理 iPhone／Safari 刷新 Web，触发任一会话 unread，确认对应项目在折叠状态显示蓝点。
- 展开项目并依次查看 unread 会话，确认最后一个 unread 被查看前项目蓝点始终保留。
