# Ariel 规格索引

从本文件开始，Ariel 的研发文档只在本仓库维护。brain-spark 中的 `codex-remote-control` 目录视为历史归档，不再作为后续修改目标。

## 状态定义

| 状态 | 含义 |
| --- | --- |
| Draft | 正在讨论，不允许开始实现 |
| Approved | 用户已确认，可以进入 TDD |
| Implemented | 规格对应实现和测试已经完成 |
| Superseded | 已被新规格取代，保留供追溯 |

## 当前规格

| 编号 | 文档 | 状态 | 目标 |
| --- | --- | --- | --- |
| 0001 | [M0 兼容性探针](0001-m0-compatibility-probe.md) | Approved（第 5 项保留 TODO） | 在当前 Mac 上确认公开 App Server 与 Desktop IPC 的真实能力 |
| 0002 | [审批拒绝语义](0002-approval-denial-semantics.md) | Approved（command/file 已实测；权限请求待真实样例） | 区分拒绝操作与拒绝并停止本轮，按原生可用决定呈现 |
| 0003 | [M1 最小链路](0003-m1-relay-mock-web.md) | Implemented（同机链路已验；物理手机待验） | Relay、Mock Agent 与响应式 Web 的单用户局域网链路 |
| 0004 | [M2 真实 Desktop Agent](0004-m2-real-desktop-agent.md) | 实施中（用户授权连续推进） | 原历史、原 owner、自动加载、发送与精确停止 |
| 0005 | [M3 原会话交互响应](0005-m3-interactions.md) | 实施中（用户授权连续推进） | 审批与补充提问，按原生请求和结果确认 |
| 0006 | [M4 断线恢复与有界容量](0006-m4-recovery-capacity.md) | 实施中（用户授权连续推进） | 重连、权威状态重建、慢消费者与大历史 |
| 0007 | [Night 界面配色](0007-night-appearance.md) | Implemented（桌面已验；手机待验） | 深色背景、白色正文、蓝色高亮与各状态可读性 |
| 0008 | [固定 6 位 Web 连接码](0008-fixed-six-digit-web-pin.md) | Approved（用户已确认） | 手机短码与 Agent 长口令分离，错误次数上限 |
| 0009 | [手机侧栏收起与灰色 logo](0009-mobile-drawer-and-logo.md) | Approved（用户已确认） | 手机侧栏点空白处收起，logo 用中性灰 |
| 0010 | [刷新页面复用 Web session](0010-browser-relay-session.md) | Approved（用户已确认） | 浏览器与 Relay 内存 session 免刷新重输连接码 |
| 0011 | [手机会话紧凑布局与 Night 视觉](0011-compact-mobile-conversation-header.md) | Approved（Web／浏览器已验；物理手机待验） | 缩小顶部区域，参考 Codex 手机版的视觉层级优化对话与输入区 |
| 0012 | [电脑浏览器紧凑会话布局](0012-compact-desktop-conversation.md) | Draft（待用户确认） | 沿用手机紧凑布局，缩小桌面顶部与输入区，侧栏按需展开 |
| 0013 | [大会话最近十回合与 64 MiB 有界同步](0013-paged-history-and-large-owner.md) | Approved（已实现并验证；物理手机旧页待验） | 默认最近 10 turn、旧历史按需分页，本机原会话上限 64 MiB |
| 0014 | [手机输入框回车换行与八行自适应](0014-mobile-composer-newline-and-eight-lines.md) | Approved（Web／物理手机已验） | 手机 Enter 仅换行，按钮发送；输入框一至八行增长并在超出后内部滚动 |
| 0015 | [工具活动默认折叠，按需查看每段调用](0015-compact-tool-activity-in-conversation.md) | Approved（用户已确认） | 一组工具调用默认单行摘要，点击查看各调用；运行状态轻量呈现 |
| 0016 | [向上滚动渐进加载会话历史](0016-scroll-triggered-history-loading.md) | Draft（暂缓；保持 0013 当前按钮加载） | 若未来启用：上滑自动逐页补充更早 turn／item，并保持阅读位置 |
| 0017 | [实时更新时保留历史阅读位置](0017-preserve-reading-position-on-live-updates.md) | Approved（用户已确认；0016 自动加载仍暂缓） | 用户上滑读历史时不被新消息拉回底部，手动旧页加载保持锚点 |
| 0018 | [黑色 Night 主题与左右分列对话气泡](0018-neutral-night-chat-bubbles.md) | Approved（用户已确认） | 近黑中性主题；用户右侧蓝色气泡、Codex 左侧深灰气泡，正文不显示角色名 |
| 0019 | [安全呈现会话 Markdown](0019-safe-markdown-conversation.md) | Implemented（自动与隔离浏览器已验；物理手机待验） | 用户／Codex 正文支持 CommonMark 与 GFM；禁用原生 HTML 和远程资源自动加载 |
| 0020 | [一键安装、配置与启动](0020-one-command-install-and-start.md) | Implemented（自动／隔离／LAN 已验；物理手机待验） | 一个脚本支持接入指定 Relay 或同机启动 Relay + Agent；提供 Agent 自动安装文档 |
| 0021 | [每回合一个图标折叠全部工具活动](0021-single-icon-tool-activity.md) | Implemented（自动已验；手机待验） | 默认隐藏全部已验证工具记录；每 turn 一个图标按需展开 |
| 0022 | [提高 Night 主题黑白对比](0022-crisp-black-white-night.md) | Implemented（自动／静态浏览器已验；手机待验） | 纯黑基底、纯白主文字与清晰中性层级 |
| 0023 | [会话截图的发送与展示](0023-conversation-screenshots.md) | Implemented（自动／隔离 Desktop 已验；浏览器与手机视觉待验） | 用户截图发送；双方原生与本地 Markdown 截图按需展示 |
| 0024 | [侧栏搜索原始 Codex 会话](0024-session-search.md) | Implemented（自动／真实 Codex／本机部署已验；手机待验） | 当前设备内搜索标题与可检索消息，摘要与分页，旧请求隔离 |
| 0025 | [最低版本兼容门禁与运行期失败](0025-minimum-version-compatibility.md) | Implemented（自动／当前 Desktop 只读探针／LAN 启动已验） | 版本达到最低门槛即可启动；私有 IPC 不兼容时按当前操作显式失败且不重放 |
| 0026 | [一键发现局域网地址并重启本地 Ariel](0026-one-command-lan-restart.md) | Implemented（自动／真实 LAN 重启已验） | 切换局域网后保留凭据与设备身份，自动发现私有 IPv4 并安全重启本地 Relay／Agent |
| 0027 | [二维码一次性配对登录](0027-qr-pairing-login.md) | Implemented（自动／真实 Relay 配对／本机部署已验；物理手机扫码待验） | 已登录浏览器展示短时一次性二维码；手机明确确认后获得标准 Web Session，二维码不包含 6 位 PIN |

## 设计基线

`baseline/` 是 2026-10-03 从 brain-spark 迁入的 v0.1 设计与证据快照。它定义产品目标、架构、协议草案、Adapter 边界、里程碑和验收矩阵。后续如有冲突，以最新的 `Approved` 规格为准。

M1–M4 当前实测与未验边界见 [2026-10-04 测试记录](../testing/2026-10-04-m1-m4.md)。
[MVP 验收矩阵逐项审计](../testing/2026-10-04-mvp-acceptance-audit.md)区分自动测试、真实 Desktop 与物理手机的证据边界。
[0011 手机布局专项验收](../testing/2026-10-04-0011-mobile-layout.md)记录 390×844 实际浏览器测量和物理手机待验项目。

## 规格拆分顺序

1. M0：兼容性探针与支持矩阵。
2. M1：自有 WebSocket 协议、Relay 与 Mock Adapter。
3. M2：历史读取、Desktop 实时订阅、发送与精确停止。
4. M3：审批与补充回答。
5. M4：断线恢复、有界资源与真实手机验收。
