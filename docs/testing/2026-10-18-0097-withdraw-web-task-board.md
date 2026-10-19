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

## 2026-10-19 公网部署

公网当前运行 `ariel-relay:9f64fc1-web-only`。由于部署前的 `ariel-relay:0096-20261018` 包含尚未合入 Git 的后台 task／Obsidian 能力，本次没有用 `main@9f64fc1` 的旧 Relay binary 覆盖它，而是构建派生镜像：保留 0096 Relay binary，只将 `main@9f64fc1` 构建出的 `/app/web/dist` 复制进去。

- 新旧 `/app/ariel-relay` SHA-256 均为 `1d9a83d94fc584fcef17e11c79323ce2f9a8a0cd0ae3ab413e6dfb0753583a9e`。
- 容器状态 `running/healthy`，公网 `https://www.kan.chat/healthz` 返回 `ok`。
- 公网主资产为 `/assets/index-C1aBs2jN.js` 和 `/assets/index-CASQioDl.css`。
- HTML 无 `ariel-enable-tasks` meta；JS 无「进入任务看板」和 `CHILLING · TASKS` 标记。
- `ARIEL_ENABLE_TASKS=true` 与 `ARIEL_ENABLE_CHILLING_FILE_CONTEXT=true` 保持不变，说明只撤回 Web 看板入口，没有关闭后台 task gateway 或文件上下文。
- 本机 Desktop Agent 在切换后 8 秒内更新 ready marker，并恢复到 `www.kan.chat:443` 的 `ESTABLISHED` 连接。
- 旧容器以停止状态保留为 `ariel-relay-rollback-0096-before-web-withdrawal`，可用于回滚。
