# 0143：Web 中文输入法组字期间的 Enter 提交保护

- 状态：Implemented（2026-11-09；用户在 Turn 62 明确授权修复并发布）
- 日期：2026-11-09
- 范围：Web 输入交互；不改变 Relay wire contract、数据库 schema 或 Desktop Agent

## 1. 问题与目标

浏览器在中文输入法组字时会把 Enter 同时暴露为候选确认键和 keyboard event。Chrome、Firefox、
macOS／iOS Safari 对 `isComposing`、`keyCode=229` 和 `compositionend`／`keydown` 的时序并不完全
一致。Web 若只判断一个字段，会把确认组字的 Enter 当作发送或提交。

本轮统一保护所有 Web 中可由 Enter 触发的发送／提交入口：

1. Codex 会话 composer（包括新会话、排队消息编辑）；
2. Puck composer（普通消息与引用回复共用）；
3. Codex 与 Puck 的 `ask_user` 自定义输入；两者当前只通过显式按钮提交，必须保持该语义，组字
   Enter 不能新增或触发提交；
4. 会话重命名；
5. 原生 form 的 Enter 提交入口：PIN 连接、重新连接、Passkey 首次设置和新建 Puck Bot。

本轮不改变移动端 Codex composer 的“Enter 换行、按钮发送”，不改变 Shift+Enter 换行，不改变
按钮点击提交，也不改设备选择等非文本键盘控件的 Enter 激活语义。

## 2. 统一判定

Web 提供唯一的 IME Enter helper，并以组件内 ref 跟踪 composition 生命周期。Enter 满足以下任一
条件时不得触发业务提交：

- React event 的 `isComposing === true`；
- `nativeEvent.isComposing === true`；
- React event 或 native event 的 `keyCode === 229`；
- 当前位于 `compositionstart` 与 `compositionend` 之间；
- `compositionend` 后紧接的 Safari 尾随 Enter，落在同一事件循环／短保护窗口内。

helper 必须区分“正在组字”与“compositionend 后的尾随 Enter”：正在组字时不调用
`preventDefault()`，以免阻止输入法确认文字；尾随 Enter 在原生 form 上必须阻止默认提交。保护窗口
结束后，下一次普通 Enter 恢复提交。blur 会结束活动组字，但不能绕过尚未过期的 Safari 尾随保护。

## 3. 行为与错误边界

- 桌面 Codex／Puck：普通 Enter 提交，Shift+Enter 换行；所有 IME 条件只确认输入、不提交。
- 移动端 Codex：所有 Enter 继续作为文本输入，不提交。
- 原生 form：正在组字的 Enter 不提交；Safari 尾随 Enter 被取消；普通 Enter 与按钮点击仍提交。
- 会话重命名：普通 Enter 保存；IME Enter 不保存。
- ask_user 自定义输入：继续由“回答／提交回答”按钮提交；IME Enter 只作用于输入法或文本。
- helper 不依赖 User-Agent，不读取环境变量，不记录用户输入或 composition 内容。

## 4. TDD 与验收

严格按 red → green → refactor：

1. helper 单元测试覆盖 React/native `isComposing`、composition ref、React/native `keyCode=229`、
   Safari `compositionend → keydown Enter`、保护窗口结束后普通 Enter 和 Shift+Enter。
2. Codex composer、Puck composer、引用回复、ask_user 自定义输入、重命名和原生 form 入口覆盖
   “组字不提交、组字结束后正常提交”；关键 composer 另覆盖 `keyCode=229`。
3. Playwright 使用隔离 WebSocket fixture 合成 composition events，并由 Chromium 真实 keyboard event 补发送路径，
   验证 Codex 与 Puck 不误发且之后普通 Enter 可发送。不得操作真实 Codex／Puck 数据。
4. 门禁：`npm test`、Web production build、相关 Playwright、`git diff --check`。本轮无 Go 改动，
   因此不要求 Go 门禁。

## 5. 发布与回滚

发布前确认生产 `/healthz`、当前容器／镜像、Puck DB schema v5 integrity，并制作一致性备份。构建
`linux/amd64` 不可变镜像 `ariel-relay:<main-sha>`，沿用当前生产参数替换 Relay/Web，保留
`ariel-relay:b72a806` 回滚容器。Desktop Agent 无改动，不重部署。

发布后确认 `/healthz`、页面与新 JS asset 为 200，线上 asset 包含本轮 helper 特征；用 `turn62-`
前缀临时 Bot 做 `ask_user` 卡和消息发送冒烟，完成后只删除该临时 Bot，不操作用户既有 8 个 Bot。
任一关键门禁或生产验证失败则恢复 `b72a806`，并报告失败原因。

## 6. 实现与验证记录

- 新增 `ime-enter` helper，以 ref 持有 composition 与 Safari 尾随 Enter 保护状态；不基于 User-Agent。
- Vitest：48 files／428 tests 通过；production build 与 `git diff --check` 通过。
- Playwright：Codex composer 与 Puck 引用回复两条隔离 IME 回归通过；同批 file workspace 行为测试通过。
- 扩展跑 Puck 全文件时，9 张既有截图因基线未包含 `b72a806` 已有的助理数量“2”而各差 14 像素；
  相关行为断言与本轮 IME 截图外测试均通过，本轮未修改该无关基线。
