# 0085 后台同步期间的发送按钮加载态验收

- 日期：2026-10-15
- 规格：[`0085-background-sync-send-button-loading.md`](../specs/0085-background-sync-send-button-loading.md)
- 实现提交：`5afc2c1`
- 结论：实现、Web 全量测试、真实移动 Chromium、截图基线、production build 与公网 Docker 发布均通过；物理 iPhone／Safari 待用户复验。

## 实现行为

当前会话已有 stale display、但 fresh owner snapshot 尚未恢复时，正文、历史、草稿和滚动位置继续保留。原发送按钮在原位置显示同为 `36 × 36px` 的圆形 spinner，并以 `aria-label="正在同步会话，暂不可发送"` 表达状态；点击和 Enter 均不能发送。fresh snapshot 完成后恢复原 SendIcon 和既有发送条件。

loading 状态直接派生自现有 `ownerReady`、`syncingView` 和 Agent recovery fencing，不复用发送回执 `working`、Codex `inProgress`、历史分页或只读历史状态，也不增加网络请求。`prefers-reduced-motion` 下保留图形但关闭旋转动画。

## TDD 证据

Red 阶段先扩展 WebSocket 1006 重连和 IndexedDB reload 恢复测试，旧实现均失败：按钮仍保留普通“发送”图标和 aria-label，只是 disabled。

Green 阶段覆盖：

- socket 真断开后立即显示 spinner，正文与草稿不变；
- IndexedDB 先恢复正文时显示 spinner，不出现整页 loading；
- 点击 loading 按钮和在 composer 按 Enter 均不产生 `thread.send`；
- fresh owner snapshot 后恢复 SendIcon 与发送能力；
- Codex `inProgress` 队列发送、只读历史等非恢复状态不错误显示 spinner；
- stream sequence gap 的 resync 同样使用该明确反馈。

## 自动与浏览器验证

```text
Vitest: 25 files, 207 tests passed
Playwright: 29 tests passed
Vite production build: passed
```

真实移动 Chromium 使用 `390 × 844` viewport，实际关闭 WebSocket并将 fresh snapshot 延迟 800ms。测试断言 loading 按钮 disabled、spinner 可见，并比较切换前后的 `x/y/width/height`，四项保持一致。固定截图基线见 [`background-sync-send-loading-darwin.png`](../../web/e2e/mobile-history-cache.spec.ts-snapshots/background-sync-send-loading-darwin.png)。

## 公网发布验证

公网 `www.kan.chat` 已部署：

```text
container: ariel-relay:5afc2c1 status=running health=healthy
healthz: ok
web asset: index-Br2YEIp8.js
css asset: index-ChhPMgQT.css
Relay established connections: 4
```

旧容器保留为已停止的 `ariel-relay-prev-8aecc78`，配置备份为 `/opt/ariel/config.env.pre-5afc2c1`。

## 保留复验

- 物理 iPhone／Safari 切换应用造成 socket 重建时，确认缓存正文立即可读，发送按钮在后台同步期间旋转且无法发送。
- fresh snapshot 到达后，确认按钮不改变尺寸或位置，直接恢复正常发送图标与能力。
