# Ariel 规格索引

从本文件开始，Ariel 的研发文档只在本仓库维护。brain-spark 中的 `codex-remote-control` 目录视为历史归档，不再作为后续修改目标。

本索引记录当前 macOS + Codex Desktop Adapter 的实际演进，条目中的产品名和平台名属于功能或证据范围，不代表 Ariel 的长期产品边界。provider-neutral 的产品分层与当前支持矩阵见[架构总览](../architecture/overview.md)；历史 spec 和测试记录不做追溯性改写。

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
| 0012 | [电脑浏览器紧凑会话布局](0012-compact-desktop-conversation.md) | Implemented（侧栏紧凑化、跨端圆角、桌面／移动 Chromium 与截图已验） | 沿用手机紧凑布局与 icon 操作体系，缩小桌面顶部、侧栏与输入区，侧栏按需展开 |
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
| 0028 | [手机输入框与操作栏上下分层](0028-stacked-mobile-composer-actions.md) | Implemented（自动／隔离浏览器已验；物理手机待验） | 输入文字独占上层宽度；附件在下层左侧，停止与发送 icon 在下层右侧 |
| 0029 | [手机输入字号与会话正文一致](0029-mobile-composer-font-size.md) | Implemented（自动／隔离浏览器已验；物理 iPhone 待验） | 输入文字使用与会话正文相同的 14px 字号，同时避免 iOS 聚焦时自动放大页面 |
| 0030 | [工具调用折叠文字入口](0030-tool-activity-text-toggle.md) | Implemented（自动／浏览器样式已验；物理手机待验） | 用低对比度“使用了 N 个工具”文字代替圆形箭头，展开内容与顺序保持不变 |
| 0031 | [Codex 原生后续输入队列](0031-codex-follow-up-queue.md) | Implemented（自动／真实 Codex fixture 已验；物理手机拖拽待验） | 运行中排队后续输入；支持引导、编辑、删除和拖拽调整顺序，并保持 Desktop owner 唯一执行权 |
| 0032 | [左侧会话按项目分组](0032-project-grouped-session-sidebar.md) | Implemented（自动已验；物理手机视觉待验） | 以 Codex 会话 `cwd` 为项目身份，将侧栏改为项目 → 会话两级结构 |
| 0033 | [在指定项目中新建 Codex 会话](0033-create-thread-in-project.md) | Implemented（自动／真实 Codex 空 thread 已验；物理手机待验） | 选择已有项目或绝对目录创建空 Codex thread，再交由 Desktop owner 接管 |
| 0034 | [项目会话分组默认收起与按需展开](0034-collapsible-project-session-groups.md) | Implemented（自动测试与 production build 已验；物理手机待验） | 所有项目默认最小化收起，点击项目标题独立展开或再次收起 |
| 0035 | [队列菜单、引导消息可见性与紧凑输入操作](0035-queue-overlay-steer-visibility-and-compact-actions.md) | Implemented（自动测试与 production build 已验；物理手机待验） | 修复队列更多菜单遮挡和引导消息正文丢失，并缩小手机输入操作 icon |
| 0036 | [置顶会话独立展示并从项目分组去重](0036-pinned-session-sidebar.md) | Implemented（自动／官方文档／0040 兼容已验） | 读取 Codex 原生置顶状态，在侧栏顶部独立展示，并从项目分组移除重复会话 |
| 0037 | [会话加载占位标识使用蓝色](0037-blue-session-loading-mark.md) | Superseded（由 0044 正式资产取代） | 历史上将加载占位标识改为蓝色强调色 |
| 0038 | [排队项双向拖拽排序](0038-bidirectional-queue-drag.md) | Implemented（pointer capture／lost capture 回归、390×844 Chromium touch E2E、截图与 production build 已验；物理 iPhone/Safari 待用户复验） | 修复手机 pointer capture 下排队项只能向上提前、不能向下后移的问题 |
| 0039 | [真实浏览器手机 UI 与截图回归](0039-real-browser-mobile-ui-regression.md) | Implemented（真实 Chromium touch、布局几何与两张截图基线已验；物理 iPhone/Safari 不在本规格验收范围内） | 用隔离 WebSocket fixture、真实 touch 事件和截图基线覆盖关键手机 UI |
| 0040 | [兼容 bundled Codex 的原生置顶分区](0040-bundled-codex-pinned-section-compatibility.md) | Implemented（真实 Codex／自动／移动截图已验） | 在较新 `isPinned` 字段之外兼容 0.160.0 的保留 `Pinned` section，让现有置顶会话立即可见 |
| 0041 | [会话加载标识改为可靠的蓝色矢量图](0041-vector-blue-session-loading-mark.md) | Superseded（由 0044 正式资产取代） | 历史上用 `currentColor` SVG 替代 Unicode 占位标识 |
| 0042 | [排队项更多菜单的紧凑字体与排版](0042-compact-queue-menu-typography.md) | Implemented（自动／移动截图已验） | 修复 Portal 菜单继承断开导致的 16px 原生按钮，并统一队列区视觉层级 |
| 0043 | [前一版正式 Logo 系统](0043-formal-logo-system.md) | Superseded（由 0044 取代） | 保留前一版品牌集成的历史规格 |
| 0044 | [正式“风之信使” Logo 系统](0044-wind-messenger-logo-system.md) | Implemented（资产完整性／自动／桌面与移动截图已验） | 全面替换为风之信使资产，升级组件、图标元数据、品牌文档与视觉回归 |
| 0045 | [阅读历史时自动收起会话 Header](0045-collapse-conversation-header-while-reading-history.md) | Implemented（上下两层 0px／真实移动 Chromium／截图已验） | 离开最新消息时将 Header 与输入区完全收起，把全部垂直空间交给对话历史；回到最新或切换会话后恢复 |
| 0046 | [登录页使用彩色“风之信使”主标](0046-color-login-brand-mark.md) | Superseded（彩色资产保留，尺寸由 0048 调整） | 未连接登录页改用彩色正式主版；固定 320px 要求已被后续紧凑尺寸覆盖 |
| 0047 | [README 顶部居中展示正式 Logo](0047-centered-readme-brand-header.md) | Superseded（居中规则保留，尺寸与内容由 0051 调整） | 历史上将新版 Logo 移到 README 首部；后续由精简入口页规格覆盖 |
| 0048 | [登录页彩色 Logo 恢复紧凑尺寸](0048-compact-color-login-brand-mark.md) | Implemented（桌面／移动截图、自动测试与 production build 已验） | 保留彩色正式资产，将首页 Logo 恢复到原黑白版的 128–160px 响应式尺寸 |
| 0049 | [登录页 Logo 与精简标题同排展示](0049-inline-login-brand-lockup.md) | Superseded（横向 Lockup 保留，文案由 0050 调整） | 建立 Logo 与标题同排的品牌 Lockup；原“接续 Codex”文案已被覆盖 |
| 0050 | [登录页标题改为“Agent 联络中继器”](0050-agent-relay-login-heading.md) | Implemented（桌面／移动截图、自动测试与 production build 已验） | 用产品定位标题替换“接续 Codex”，并保持桌面和手机同排展示 |
| 0051 | [面向使用者的精简 README](0051-readable-project-readme.md) | Implemented（Logo 像素透明度、README 链接与 Web 全量测试已验） | 用背景、架构、快速开始、基本使用和文档导航重写项目入口，并验证新版 Logo 透明背景 |
| 0052 | [移动端 Markdown 表格列字体一致](0052-uniform-mobile-markdown-table-type.md) | Implemented（移动截图、自动测试与 production build 已验） | 关闭表格 text inflation，让全部表头与单元格继承统一字号和行高，并补移动端截图回归 |
| 0053 | [回到最新时稳定恢复会话界面](0053-stable-latest-chrome-restoration.md) | Implemented（自动／真实移动 Chromium／截图已验） | 在 Header／输入区域展开期间持续锚定最新消息，消除布局变化造成的反复收放抖动 |
| 0054 | [面向 Agent App 的中立产品定位](0054-provider-neutral-public-positioning.md) | Implemented（文档契约／真实浏览器品牌页／build 已验） | 将长期定位与当前 macOS + Codex Desktop Adapter 支持矩阵分层表达，避免用首个实现限制产品边界 |
| 0055 | [Relay + Web 容器镜像](0055-containerized-relay-image.md) | Implemented（默认 build／隔离 smoke／全量回归已验） | 用多阶段 Dockerfile 构建非 root 的 Relay + Web 镜像，Desktop Agent 保持在桌面宿主机运行 |
| 0056 | [超高分辨率会话图片按需生成安全预览](0056-large-conversation-image-preview.md) | Implemented（真实大图／自动／移动 Chromium／截图已验） | 保留来源像素硬上限，在 Desktop Agent 端把可接受的大图缩成移动端安全预览，并显示明确失败原因 |
| 0057 | [手动滚到最新时可靠恢复会话界面](0057-manual-scroll-restores-conversation-chrome.md) | Implemented（自动／移动 Chromium／截图已验） | 记住连续滚动方向并识别触摸／滚轮意图，修复到底后仍不恢复 Header 与输入区的问题 |
| 0058 | [纯图标“回到最新”控件](0058-icon-only-return-to-latest-control.md) | Implemented（自动／移动 Chromium／截图已验） | 用标准向下箭头替换右下角可见文字，保留无障碍名称与原有恢复行为 |

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
