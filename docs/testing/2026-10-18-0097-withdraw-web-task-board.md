# 2026-10-18：0097 撤回 Web 任务看板入口

对应 [0097](../specs/0097-withdraw-web-task-board-entry.md)。本次只撤回 Web UI，保留默认关闭的 Relay／Desktop Agent／chilling 后台 task 能力。

## TDD 证据

Red 阶段确认两项失败：

1. legacy task meta 为 true 时，当前 Web 仍显示「任务看板」按钮。
2. Relay 仍把后台 task flag 渲染到 Web HTML。

Green 后：

- Web 无模式栏、侧栏看板入口、`TaskBoard` 挂载和 task client method。
- `index.html` 无 `ariel-enable-tasks` meta；Relay 不注入 task UI 状态。
- 后台 `--enable-tasks` 仍只控制 task gateway，不改变 Web UI。

## 自动与浏览器验证

```sh
cd web
npm test
npm run build
npx playwright test e2e/web-task-board-withdrawn.spec.ts
cd ..
go test ./... -count=1
```

- Vitest：30 文件、237 项通过。
- production build：通过。
- Go：全仓通过。
- Chromium 专项：手机 390×844、桌面 1440×900 共 2 项通过；设备 fixture 刻意宣告 `taskManagement`／`taskExecution`，页面仍无入口且 WebSocket 无 `task.*` 请求。
- 截图基线：[手机](../../web/e2e/web-task-board-withdrawn.spec.ts-snapshots/conversation-only-mobile-darwin.png)、[桌面](../../web/e2e/web-task-board-withdrawn.spec.ts-snapshots/conversation-only-desktop-darwin.png)。两张均已人工检查。

完整 Playwright 40 项中 37 项通过。本规格新增 2 项及会话草稿、缓存、文件引用、恢复相关项均通过；其余 3 项是无关的旧品牌／Passkey 截图漂移与登录 Logo 旧文件名断言，未修改其基线。

物理 iPhone／Safari 留给用户复验，不以 Chromium touch viewport 代替。
