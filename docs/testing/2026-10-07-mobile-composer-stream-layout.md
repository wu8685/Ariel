# 手机输入框与 streaming 布局稳定性回归

- 关联规格：[0014 手机输入框回车换行与八行自适应](../specs/0014-mobile-composer-newline-and-eight-lines.md)、[0017 实时更新时保留历史阅读位置](../specs/0017-preserve-reading-position-on-live-updates.md)。
- 来源：用户在物理手机上报告，消息发送后空输入框比正常单行稍高；Codex streaming 期间正文持续上下抖动。

## 根因

手机 composer 的自适应高度 effect 依赖整个 `view`。每个 streaming update 都会创建新的 `view`，因此即使草稿没有变化，textarea 仍反复执行“清除高度、设为 `auto`、读取 `scrollHeight`、回写高度”。这会在正文底部跟随逻辑调整 `scrollTop` 的同时反复改变正文可用高度。

空草稿还会受到 placeholder 影响：运行中提示更长，停止按钮又压缩 textarea 宽度；手机浏览器可将换行后的 placeholder 计入 `scrollHeight`，使空输入框被错误扩为两行。两者叠加后表现为发送后输入框偏高，以及 streaming 时正文随 composer 重排而上下抖动。

## TDD 与修复

1. 新增“运行中 placeholder 换行时空草稿仍为 44px”测试，修复前实际得到 68px。
2. 新增“同一 active turn 的 streaming update 不重测未变化草稿”测试，修复前每个 update 都多读取一次 `scrollHeight`。
3. 空草稿直接固定为手机单行高度并归零内部滚动，不再测量 placeholder。
4. 高度计算改为 `useLayoutEffect`，只依赖草稿和 active turn 身份；窗口 resize 仍由既有监听处理。stream 文本变化不再触发 composer 布局重算。

## 验证

- Red：两个新增测试分别以 `68px != 44px`、stream update 多一次高度测量失败。
- Green：`App.test.tsx` 49/49 通过；Web 全量 105/105 通过；production build 通过；`go test ./...` 全包通过。
- 隔离 Chromium 390×844 视口：九行草稿发送后，composer 只发生一次预期的 `212px → 44px` 收缩；后续多个 Mock streaming update 中没有再次 resize，正文容器保持 710px，页面入口未操作真实 Codex 会话。
- 物理手机 Safari 的最终触感与真实 streaming 仍由用户在部署后的局域网页面复验；自动化与隔离浏览器结果不替代物理设备验收。
