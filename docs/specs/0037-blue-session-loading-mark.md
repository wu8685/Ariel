# 0037：会话加载占位标识使用蓝色

> 本规格已由 [0044 “风之信使”正式 Logo 系统](0044-wind-messenger-logo-system.md)取代；加载标识现在直接使用正式反白微标，不再运行时染色。

- 状态：Superseded（由 0044 取代；以下内容仅为历史记录）。
- 范围：会话内容尚未同步完成时，正文区域中央的 `✳` 占位标识。

## 用户可见行为

1. 打开会话、等待原始历史或 owner snapshot 时，中央占位标识使用 Ariel 的蓝色强调色 `--color-accent`。
2. 标识的字符、尺寸、位置，以及“正在同步会话…”等文案保持不变。
3. 连接状态圆点继续使用既有在线／警告语义色，不随本次调整改变。

## TDD 与验收

1. 先修改主题契约测试，要求 `.empty-symbol` 使用 `var(--color-accent)`，并实际运行到 red。
2. 只修改加载占位标识的颜色 token，完整 Web tests 与 production build 通过。
3. 推送后重启本地 Ariel，并以 LAN health endpoint 验证部署。

## 非目标

- 不修改 Ariel 品牌 logo、状态圆点、按钮或消息气泡配色。
- 不重做加载动画或会话同步流程。
