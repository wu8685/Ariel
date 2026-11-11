# 0130：Puck 个人助理编排层与跨主机 Codex 任务管理

- 状态：Approved（P0 范围；2026-11-03 起草；同日用户已就 Q1／Q2／Q4／Q10／Q20／Q21／Q22 做出决定并纳入本稿；同日依据 Mac mini 只读探针与代码核对补充 §4.2–§4.4（Q3、Q14 已决），同日用户决定 Q24（brain thread 由大脑主机 Agent 自持的常驻 app-server 长期持有），据此 Q13 定为动态工具经 Agent 中转、Q15 已决，并按用户要求新增体验原则（§1.1）与界面规范（§1.2），其中 Q2 的部署形态已被“同一进程、第二入口”的决定修订；同日完成 P0 第 0 步写探针（Q27／Q28 已有结论、Q25 实测；Q23 Desktop follower 显式 policy 无法取得兼容读回，结论 unverified，按 §4.4／B10 拒绝自动派发，见 §4.2.2）；22:21 用户确认 Q27：brain thread 在 Desktop 可见，作为 v1 已知外观行为接受；据此本规格批准进入 P0 实施（§16），P1 及以后各阶段开工前仍需逐阶段确认；2026-11-09 用户批准将活跃 Bot 上限从 10 调整为 50，并要求发布；2026-11-11 隔离写探针通过后，将 Desktop `26.1103.60933`／Codex CLI `0.167.0-alpha.3.2` 作为精确组合加入 brain 版本门禁，不外推相邻版本，见 §4.2.2；2026-11-11 按产品负责人要求补充 §6.1.1：同一 Bot 对同一设备与目录默认续投已有执行线程；同日用户要求实现 P1 切片：执行任务监听、只读 `watch_thread`、任务事件唤醒与限速排队、`get_task_status` 读现场 runtime，以及页内系统通知。完整 VAPID Web Push 仍未实现，见 §9.1 与 §10.11；2026-11-11 用户要求实现分层 Bot 上下文与任务收尾：系统上下文由代码维护，每 Bot 自定义上下文存 SQLite，创建与右键「编辑」均可改；改自定义上下文不轮换 brain thread，下一轮受信 envelope 生效。`finish_task` 使工具集版本变为 8，见 §6.1.2；2026-11-11 用户批准把每个 Bot 改成拥有 Full Access 的主助理：brain 使用 `never` + `danger-full-access`，对用户主动开口只走 `send_message`／`ask_user`／`share_result`，子会话结果各自唤醒一次，删除 Bot 时清掉它的 Puck 记录，子会话消失时唤醒一次，见 §6.1.3 与 §10.12。工具集版本变为 9；2026-11-11 产品负责人要求交接时由代码附上原始请求和被引用会话，工具集版本变为 10，见 §6.1.4）。
- 日期：2026-11-03。
- Issue：尚未创建。关联：0091（后台 task gateway 与跨主机路由）、0095／0096（领取 → WIP → 续投同一 thread）、0097（Web 看板撤回）、0059／0084／0112（Passkey 与会话认证）、0100（Relay 双 turn hot cache）、0107（Agent transport 恢复）、0109（Web wire 向后兼容）、0098（补充问题卡片）、0069（确认发送）、0002（审批拒绝语义）；依赖风险 #4、#64、#63。
- 术语：**助理** 指用户创建的一个助理实例（有独立 id、名称、人设、大脑、记忆、例行、任务与通知设置）；**Puck 模块**（下文亦简称 **Service**）指编译进 Relay 二进制、由配置开启的 `internal/puck` 模块，承载全部助理；它是 Relay 进程的第二个入口，**不是独立进程**；**Brain** 指某个助理在其大脑主机上的 Codex 会话（brain thread）；**Desktop Agent** 指 Ariel Desktop Agent（`role=agent`）；**执行 thread** 指被派发任务的目标主机上的 native Codex thread；**关键动作** 指 §5 定义的、必须由用户授权的动作。各术语不得混用，延续 AGENTS.md 的术语约束。
- 现行界面：侧边栏分组、建群、名称搜索、头像页和默认收起的 Bot 配置栏以[功能指南](../guide/features.md#puck)、[0146](0146-classic-session-readiness-and-puck-group.md)、[0147](0147-grok-avatar-option-parity.md)、[0148](0148-puck-sidebar-sections.md)、[0149](0149-puck-avatar-sheet.md) 和 [0150](0150-puck-bot-config-column.md) 为准。下文 UI1 的底栏「新建助理」，以及 UI8 的十四种尖耳轮廓和信息栏内嵌选择器，是 2026-11-03 草案，不表示当前 main 仍是这个界面。注视时更靠近注视方向的那只眼更窄，见 UI8 第 3 条。

## 1. 用户结果、体验原则与界面规范

用户已经能通过 Ariel 在浏览器里逐个继续各主机的 Codex 会话，但“这件事交给哪台机器、现在做到哪、哪里卡住在等我”仍要靠用户自己逐台、逐会话查看和记忆。

目标体验：用户在 Ariel Web 中与**一个或多个助理** 对话，可随时新建助理（例如“工作助理”“家里服务器管家”），每个助理：

1. 理解自然语言请求，自主选择目标主机与工作目录，在该主机的 Codex 上新建或续用执行 thread 派发、追加、停止工作，**默认不逐次请求确认**；
2. 仅在遇到**关键动作**（§5）时找用户授权；Codex 原生的命令／文件／权限审批与补充提问始终由用户本人决定；
3. 在后台持续跟踪已派发任务，不依赖用户开着页面，也不依赖大脑主机在线；
4. 在任务完成、需要审批／授权／回答或失败时通过 **Web Push** 主动通知用户，点击直达对应助理对话、授权卡片或执行 thread；
5. 拥有独立的长期记忆与定时例行（routines）。

推理能力不引入新的云端 LLM，依托用户为每个助理指定的主机（例如 Mac mini）上的 Codex。

### 1.1 体验原则（E1–E14）

用户要求 Puck 的使用体验尽量接近“对话式个人助理”：像和一个靠谱的助手聊天，而不是操作一个任务系统。以下原则是产品层要求，每条都有可测试的用户可见行为，并映射到本规格的组件与状态机；与实现便利冲突时以本节为准，除非另经用户决定。

| # | 原则 | 可测试的用户可见行为 | 落点 |
| --- | --- | --- | --- |
| E1 | 即时确认，绝不沉默 | 用户发出消息后 ≤ 500 ms 出现“正在处理”指示（模块本地产生，不等 Brain）；目标 ≤ 2 s 出现一条简短确认气泡，点明第一步（如“好的，我先去 Mac mini 上看一下 Ariel 的测试”）；3 s 内 Brain 仍无文本时模块补一条模板确认（“收到，正在处理”），Brain 文本到达后照常显示；任何失败都以消息说明，不留空白 | §4.2 Brain 指令、§6.1、§9 `acknowledged`、§15 P0 |
| E2 | 默认后台执行 | 耗时工作一律成为可跟踪任务（§9）；派发后对话立即可继续输入，用户可同时与同一或其他助理聊别的；任务在关闭页面后继续 | §9、§7.1、§1.2 UI2 |
| E3 | 里程碑更新，而非解说 | 进度消息只在里程碑出现：已在 X 主机开始、被阻塞（需审批／授权／回答／离线）、有结果（完成／失败）；命令、工具调用与中间推理收进可展开的“工作了 N 秒 · 执行 M 步”详情，不在对话中逐条刷屏 | §9 里程碑映射、§6.1 `post_update`、§1.2 UI2 |
| E4 | 主动送达 | 任务完成、失败或需要用户时，助理 **同时**在对话中发结果消息并发 Web Push；用户无需轮询或刷新 | §9、§10、§15 P1 |
| E5 | 审批卡片在对话中 | 关键动作（§5 B 类）与 Codex 原生审批（A 类）都以对话内卡片呈现：做什么、在哪台主机哪个目录、风险说明、一键批准／拒绝、剩余有效期；点 Push 直达同一卡片 | §5.3、§7.4、§1.2 UI3 |
| E6 | 问题卡片 | 需要用户做决定时，展示**一个**问题 + 可点选项（可含推荐项）+ 自由输入，而不是一段散文；用户直接发新消息即视为跳过，卡片收起 | §6.1 `ask_user`、§1.2 UI3 |
| E7 | 记忆 | 跨对话记住偏好与事实；保存时在对话中顺带一句（小标签“已记住：……”，可撤销）；设置中可查看／编辑／遗忘 | §6.1 记忆工具、§1.2 UI6、P2 |
| E8 | 例行 | “每天早上……”“……的时候告诉我”自动变成 routine，或以一句话询问（问题卡片）后创建；routine 有列表，可编辑／暂停 | §11、§1.2 UI6、P2 |
| E9 | 语气 | 温暖、简洁、口语化；先给结论；短消息、一段一事；默认中文（按用户设置） | §4.2 人设硬规则、§15 P0 文案检查 |
| E10 | 多个助理 | 每个有名称、头像、人设、独立对话、记忆、例行；侧栏切换；未读与待处理角标 | §8、§1.2 UI1 |
| E11 | 结果是链接／附件 | diff、文件、PR、执行会话以可点击条目呈现（标题 + 主机 + 简述），点开走 Ariel 现有查看器；不在对话中倾倒大段原文 | §6.1 `share_result`、§1.2 UI3 |
| E12 | 随时停止 | 发送“停”／“停止”或点停止按钮：中断该助理当前 brain turn 及其派发的所有进行中任务，并回一条消息列出停了什么、哪些未能确认 | §7.3 `puck.stop`、§9、§1.2 UI4 |
| E13 | 状态诚实 | 未经验证不说“完成”；任务卡片只显示状态机的真实状态；`unknown` 显示“状态待确认”，不显示成功 | §9、§14 |
| E14 | Puck 是活的 | 每个助理都有确定且可辨认的林间精灵团子头像：十四种柔软轮廓直接配一对靠近的黑色、平行竖向胶囊眼，只用一对根部嵌入轮廓并随头部运动的细长小尖耳呼应《仲夏夜之梦》的 Puck（Robin Goodfellow），不使用白眼眶、叶冠、花朵、嘴或眉毛；生命感来自各实例独立、柔和连续的整头转向、脸部八向移动、透视眼形变化和随机眨眼，动作成串、停顿自然且中途换向不跳回中心；动画不替代文字状态，并尊重 reduced motion、标签页可见性与视口可见性 | §1.2 UI8、§8、§15 P0 |

### 1.2 界面规范（UI1–UI8）

以下是产品层界面要求，只约束用户看到与操作的东西，不规定具体组件实现；视觉复用 Ariel 现有主题。

#### 线框（桌面 ≥ 1024 px）

```text
┌──────────────────┬─────────────────────────────────────────────┬──────────────────────────┐
│ [Puck] [会话]     │  ◉ 工作助理 · 大脑：Mac mini ●在线      [停止] │ 概览 任务 例行 记忆 主机 通知 │
│──────────────────│─────────────────────────────────────────────│──────────────────────────│
│ ◉ 工作助理    (2) │                     帮我把 Ariel 的测试跑一遍 ▸│ 任务（2 进行中）           │
│   已在 Mac mini…  │                                   👍 21:14 │ ┌──────────────────────┐ │
│ ◎ 家里服务器管家   │ ◂ 好的，我先去 Mac mini 上跑 go test。     │ │ Mac mini · Ariel      │ │
│   昨晚备份完成     │ ┌ 任务 ───────────────────────────────┐    │ │ ● 运行中  打开会话 ›   │ │
│                  │ │ Mac mini · ~/code/Ariel             │    │ └──────────────────────┘ │
│                  │ │ ● 运行中 · 2 分钟      打开会话 ›     │    │ ┌──────────────────────┐ │
│                  │ └──────────────────────────────────────┘    │ │ MacBook · web         │ │
│                  │ ▸ 工作了 42 秒 · 执行 6 步                  │ │ ⚠ 需要审批            │ │
│                  │ ┌ 需要你批准 ─────────────────────────┐    │ └──────────────────────┘ │
│                  │ │ git push origin main（推送）          │    │                          │
│                  │ │ Mac mini · ~/code/Ariel · 风险：高    │    │                          │
│                  │ │ [批准一次] [拒绝]        29 分钟后过期 │    │                          │
│                  │ └──────────────────────────────────────┘    │                          │
│ [+ 新建助理]      │ ┌──────────────────────────────┐ [📎] [发送]│                          │
│                  │ │ 给 工作助理 发消息…              │         │                          │
└──────────────────┴─────────────────────────────────────────────┴──────────────────────────┘
手机（< 768 px）：侧栏收为抽屉（左上角按钮）；对话全屏；右侧信息栏改为从标题进入的全屏页；主屏幕 PWA。
```

#### UI1 布局

现行左侧栏见 [0148](0148-puck-sidebar-sections.md)：自建分组与「未分组」、长按后拖动、加号菜单里的「建群」，以及空白处右键「建分组」。下面保留草案。

1. 左侧栏列出助理：头像、名称、未读角标（待授权／审批另以醒目角标）、最后一条消息预览（≤ 1 行）、大脑主机离线标记；底部“新建助理”按钮。侧栏顶部的“Puck｜会话”切换在 Puck 视图与 Ariel 现有会话视图之间切换，记住上次选择；Puck 未开启时不出现切换（§4.1.4）。
2. 主区域：每个助理 **一条连续对话**（不分会话）；顶部显示名称、大脑主机与在线状态、停止按钮（仅在工作中显示）。
3. 右侧信息栏（按助理）：标签页 **概览**（头像、名称、描述／人设、大脑主机与状态）、**任务**（已派发任务及实时状态）、**例行**、**记忆**、**主机与权限**（允许的主机／目录、关键动作策略）、**通知**（本设备通知开关与类型）。可收起。0150 起，Bot 配置栏在日常会话里默认收起，由侧栏 Bot 行右键「编辑」打开；不再按 1280 px 自动展开。
4. 手机：侧栏收为抽屉，对话全屏，信息栏为全屏子页；支持“添加到主屏幕”的 PWA（§10.7）；安全区、键盘弹出时输入框不被遮挡。

#### UI2 消息

1. 消息：复用 Ariel 现有 `.message`／`.message-body` 与主题变量。用户消息靠右，使用 Ariel 的蓝色 `--color-primary` 气泡、`--color-on-primary` 前景色、圆角与间距；助理消息靠左，沿用 Ariel 助理消息的排版尺度，但在 Puck 固定黑色主题中保持无外层气泡正文。支持 Markdown、行内代码、代码块、Mermaid（复用现有 Markdown／Mermaid 渲染）；链接显示为带标题的可点击链接，不显示裸长 URL。
2. 助理偏好短消息；一次回复中的多个段落（Brain 的多条 agent message）显示为多个连续正文块，不合并成大卡片。历史与完成态正文、问题卡、授权卡和任务卡均不显示头像；只有正在工作的处理状态行显示头像，不能在卡片上方留下单独占行的空头像。
3. 表情回应：助理可对用户消息加一个表情回应（如 👍、👀），显示在用户气泡角落，用于轻量确认（E1）。
4. 工作中：助理忙碌时在对话底部显示“正在输入／处理”指示，直到本轮结束或出现需要用户的卡片。
5. 过程折叠：工具调用、命令、中间状态不逐条展示，折叠为一行“工作了 N 秒 · 执行 M 步”，展开后显示有界、脱敏的步骤列表（不含凭据）。
6. 时间戳：按用户时区显示（默认 Asia/Shanghai），同一分钟内的连续消息合并显示一次；悬停／长按显示完整时间。
7. 对话消息必须以当前 `assistantId` 为显示归属。切换或新建助理时先原子清空／替换旧助理消息，再读取新助理历史；旧 history poll、watch event、发送回执、卡片回执或回应回执即使晚到也不得写入当前助理的对话。

#### UI3 对话内卡片

所有卡片在对话流中内联显示，状态变化时原地更新（不追加重复卡片），并有可访问的按钮名称。

1. **问题卡片**：问题文本 + 选项按钮（主要／危险样式区分，可单选或多选）+ 自由输入框；用户回答后卡片折叠为“你选择了……”；用户改发普通消息则卡片标记为“已跳过”并收起。
2. **授权卡片**（§5.3）：动作摘要、主机、`cwd`、命中规则与风险说明、可展开的完整 prompt、“批准一次”（主要）／“拒绝”（危险）、剩余有效期；过期后变灰。
3. **Codex 审批卡片**（A 类）：复用 Ariel 现有审批／补充问题卡片组件与语义（0002、0098），只在外面加“来自任务：……”的上下文行。
4. **任务卡片**：主机、项目（目录名）、状态标签（与 §9 状态一一对应的颜色与文案）、耗时、“打开会话”链接（跳转到 Ariel 会话视图中的执行 thread）。
5. **附件卡片**：文件、diff、PR、执行会话链接（E11）：图标 + 标题 + 主机／仓库 + 简述；点开走现有文件／文档查看器或外链。
6. **通知权限卡片**：首次有任务派发后，若本设备未开启通知，助理在对话中发一张“开启通知，任务完成时提醒你”的卡片（按钮在用户手势内申请权限）；iOS 非主屏幕模式下改为添加到主屏幕的引导；拒绝后不再反复出现。

#### UI4 输入框

底部使用一个大圆角深灰输入框：文本输入支持多行、Enter 发送、Shift+Enter 换行；发送为白底黑色的圆形图标按钮。图片附件沿用现有限制；助理工作中标题栏出现圆形停止图标按钮（E12），停止后隐藏。草稿按助理保存（复用现有草稿机制）。

#### UI5 新建助理

1. 轻量表单（一屏）：名称（必填）、头像（可选，默认生成）、描述／用途（一句话，作为人设种子）、大脑主机选择器（只列在线且具备能力的主机）、允许的主机与目录（默认只含用户在此处勾选的主机与目录，不预填，可稍后改）。
2. 提交后立即进入该助理的对话，显示“正在准备”；准备完成后由 **助理主动**发一条简短问候与入门引导（自我介绍、能做什么、一个问题卡片问用户第一件想交给它的事）。准备失败时在同一对话中说明原因并提供“重试”。
3. 表单另有可选的「自定义上下文」（最多 4000 个 Unicode 字符）。它只补充该助理的习惯，不能改写系统安全规则。右键 Bot「编辑」打开的配置栏可以之后再改，保存后不更换大脑会话，从下一轮对话生效。系统上下文不在此编辑。

#### UI6 设置（信息栏标签页内完成）

1. 通知：本设备开关、按类型开关（§10.4）、“通知中显示摘要”。
2. 关键动作策略：B 类规则开关与阈值（§5.2），A 类显示为不可关闭。
3. 记忆：列表（内容、来源、更新时间）、编辑、遗忘、全部导出。
4. 例行：列表（名称、计划的自然语言描述、下次运行时间、上次结果）、编辑、暂停／恢复、删除。

#### UI7 视觉

Puck 视图固定使用独立的深色外观，不跟随操作系统或 Ariel 当前浅色主题：页面背景为近黑色（`#000`–`#0d0d0d`），侧栏、信息栏、卡片和输入区为 `#161618`–`#1c1c1e`，分隔与描边约 `rgba(255,255,255,.08)`；系统字体使用 `-apple-system, "PingFang SC", system-ui`，正文约 15 px、正文白色、次要信息中灰。侧栏行无分隔线，使用“头像 + 粗体白色名称 + 单行灰色预览”，选中行显示圆角深灰底。卡片圆角约 16 px；主按钮为白底黑字药丸，次按钮为深灰底白字药丸，危险按钮为红色；均有清晰的 hover／active／disabled／focus 状态。320／390／1280 px 三档布局无横向滚动；颜色不作为状态的唯一表达。即使浏览器声明 `prefers-color-scheme: light`，Puck 根节点的计算样式与截图仍保持深色；该约束只作用于 Puck 视图，不改变 Puck 关闭时或 Ariel 其他视图的字节与外观。

#### UI8 动态头像

现行头像没有存在光环。注视时，更靠近注视方向的那只眼更窄，见下面第 3 条。头像页使用固定的 4+4+5 带眼睛形状和白色起头的 6+5 共 11 色；「上传」与「重置为默认」会真正保存，「生成」只作说明。见 [0147](0147-grok-avatar-option-parity.md) 与 [0149](0149-puck-avatar-sheet.md)。第 1、6 条里的十四种尖耳轮廓和信息栏内嵌选择器仍是 2026-11-03 草案。

1. **实现与身份**：头像使用纯 SVG + CSS／TypeScript 组件，不引入重型图形依赖。默认形状与颜色由 `assistant_id` 确定性派生，同一助理跨设备、刷新与重启保持一致；形状包含圆、云团、倾斜圆角方块、四角星、四叶花、心形团、波浪花、梨／水滴、胶囊、圆角三角、倾斜圆角矩形、尖刺星、圆角六边形、南瓜（带小柄）十四种柔和轮廓。每种形状只带一对按自身宽高适配的细长小尖耳，耳色比身体略深，耳根须嵌入并由身体轮廓遮盖一部分，使尖耳在 28–96 px 下明确长在头上而不悬浮，并与身体处于同一头部变换组；眼睛是直接绘制在身体色块上的两条黑色竖向圆角矩形胶囊，左右边保留接近直线的中段、上下端圆润，不使用完整椭圆，也不绘制白眼眶或瞳孔裁切区。两眼中心距约为此前版本的 60–70%，且必须始终互相平行：静止时可作为整张脸共同倾斜约 20–30°，但不得分别旋转成内八或外八。任何形状与状态都不绘制叶冠、叶子、花朵、黄色点、嘴或眉毛，不能画成写实人物、复杂脸孔或细碎插画。用户可在右侧信息栏更改形状与颜色，选择结果随助理写入 SQLite，并通过既有 `puck.update`／协议字段读写；未设置时不把派生值写回数据库。
2. **出现位置**：同一头像用于侧栏行、对话标题、处理中状态行和信息栏大尺寸预览；历史会话与已经完成的 assistant 文本／卡片左侧不显示头像，只有 `processing`／`greeting_processing`／`acknowledged` 等正在工作的状态行显示头像，最终回复落地后该行头像随处理态一起消失。侧栏同时显示最后一条消息预览，并在头像上叠加未读／需关注角标。PWA manifest／Web Push 的全局静态图标不能按助理动态变化，P0 保留 Ariel 应用图标；P1 在通知 payload 中携带无敏感信息的 avatar descriptor，由 Service Worker 生成或选择缓存的同形状／颜色 PNG 作为通知图标。
   尺寸角色互相独立：侧栏列表头像约 96 px、列表行至少略高于头像；处理中状态行头像至少 56 px；标题头像保持 32 px；形状选择器保持约 28 px；信息栏 hero 保持 96 px。放大列表或消息头像不得连带放大其它角色。
3. **自主动画与状态表情**：头像无需用户交互也有生命感。静止姿态由实例确定性派生：两只靠近的平行胶囊眼作为一张脸共同偏向某个方向并共同倾斜约 20–30°；朝向侧的近眼略窄略短、远眼更大，以表现球面透视。左右、上下和四个对角都一样：比较头部与脸部转动之后的位置，更靠近注视方向的那只眼更窄，不得弄反。转头时脸在上、下、左、右和四个对角方向间平移约头部半径的 25–40%；整个头部轮廓与尖耳在同一变换中向该方向平移头宽的约 6–9% 并旋转约 ±18–26°，但不改 path、形状或颜色。除单次转向外，调度器偶尔选择由多个连续姿态组成的水平环绕、垂直环绕或对角环绕路径，使头部围绕中心沿较大弧度平滑扫视；环绕段之间不回到中心。动作中的两眼用连续 `scale` 从细长竖胶囊共同变为面积约 1.5–2 倍、更大更圆的椭圆；朝任一方向看时，只用 scale 收窄转动后更靠近注视方向的那只眼（`scaleX` 更窄、`scaleY` 略短），远眼保持原尺寸，且两眼仍保持平行。不能只按水平分量或未旋转的左右下标选择。转向／歪头用 600–800 ms、回正用 700–900 ms，头部、脸部和眼形必须共用同一时长与 `cubic-bezier(0.45, 0, 0.25, 1)` 柔和 ease-in-out 时间轴，到位回弹不得超过 2–3%；从当前方向连续换向或中途打断时从浏览器当前插值姿态续接，不得先跳回中心。普通到位后停留约 0.5–2 秒，环绕路径的相邻段连续衔接；动作可连续成串，回到静止姿态后随机停顿数秒。眨眼时两只黑色胶囊一起在垂直方向压扁并恢复，全程约 220–280 ms，环绕期间也可偶尔眨眼或短间隔双眨。每个实例使用 `assistant_id` 与实例序号派生独立随机序列，不能同步。状态表情只改变脸部／眼形姿态与文字、角标；无状态或未知状态按 `idle` 呈现，不能伪装成成功。
4. **交互**：桌面端指针靠近时整张脸朝指针方向移动，并可带动头部轻微转向；点击或轻触可触发狡黠的单眼眨眼与少量纯几何闪光。闪光只改变 `transform`／`opacity`、不接收指针事件、短时自动清除；不得使用花瓣、叶片或花朵粒子。头像不可拖拽，不拦截侧栏行、消息或设置控件的主操作。
5. **性能与可访问性**：全部头像共用一个全局事件调度器与单个 `setTimeout` 链，按下一最早事件唤醒；不得为每个实例常驻 timer，不使用 `setInterval` 或 `requestAnimationFrame` 循环。动画过程不触发 React render，调度器经 ref 直接设置 `transform`／`opacity` 或调用 Web Animations API；所有 SVG 变换使用一致的 `transform-box`／`transform-origin`，头部、脸部和眼睛只动画 `transform`／`opacity`。仅在 transition 实际运行期间临时设置 `will-change: transform`，结束或取消后立即移除；空闲等待期除单个全局 timeout 外零工作，不使用无限 CSS 动画或长期 `will-change`。`document.visibilityState=hidden` 时清除 timer 并暂停调度，恢复可见后重新错峰；`IntersectionObserver` 判定不在视口的头像不参与调度。`prefers-reduced-motion: reduce` 下只保留低频随机眨眼。根节点使用 `role="img"`，`aria-label` 同时包含助理名称与中文状态；角标另有可读文本，颜色不作为唯一状态信号。
6. **形象选择器**：右侧信息栏按“大头像预览 → 十四种形状网格 → 两排颜色色块”排列，不使用下拉框或原生颜色输入。形状预览使用当前颜色，选中项有圆角描边环；颜色按参考图的白、棕、红、橙、琥珀、绿、青绿、蓝、紫、粉、灰十一项高对比色展示，选中项有外环（用户文字写“12 个”，但参考图与逐项枚举当前均为十一项，若补充第十二项再扩展）。二者均使用 `role="radiogroup"`／`role="radio"` 与 `aria-checked`，支持 Tab、Enter／Space 和组内方向键。选择后大头像立即同步，并通过 `puck.update` 持久化；请求失败时回退到最后确认值并显示错误。320／390 px 下自然换行，不产生横向滚动。
7. **验收**：单元测试覆盖 `assistant_id → 默认形状／颜色` 的确定性、十四种形状与十一种明确颜色全集、每种形状仅两个细长尖耳且耳根与身体同属头部变换组、任何状态都无白眼眶／裁切区／叶冠／花朵／花瓣／嘴／眉毛、两只黑色胶囊眼始终平行且眼距收窄、合法自定义覆盖、状态 → 表情映射、未知状态回退、选择器选中态与键盘选择、点击持久化、纯闪光自动清除，以及用 fake timers 验证多头像只存在一个待执行 timeout、隐藏时 timer 清零、视口外实例不调度、八向脸部移动与整头转向、近侧眼透视、眼形面积增长 1.5–2 倍、600–900 ms 同步时间轴、220–280 ms 单眨／双眨、reduced-motion 只眨眼、空闲期无 rAF 与 React re-render；测试还须静态禁止头像 body 的无限动画和长期 `will-change`，并验证 `will-change` 只在 transition 期间存在。Playwright 覆盖 320／390／1280 px 冻结动画后的深色截图，其中 390 px 主基线保持聊天界面、信息抽屉另存基线；另产出同一头像静止、左上、右下、眨眼等姿态的动作分镜基线，以及按约 16 ms 间隔覆盖完整转头时间轴的 30 帧以上连帧图，检查连续换向无跳回中心。浏览器测试还覆盖小头像轮廓／尖耳／黑色胶囊眼、选择器全集与持久化、同组助理消息只显示一次头像、用户气泡复用 Ariel 变量及标签页隐藏暂停。

#### 界面验收要点（Playwright，真实浏览器）

每条映射到 §15 的阶段验收，均需截图基线 + 行为断言（不只截图）：

- 布局：三栏在 1280 px 可见；390／320 px 侧栏为抽屉、对话全屏、信息栏为子页；320／390／1280 px 在浅色／深色浏览器偏好下截图均为固定深色且无横向溢出。
- 消息：用户右侧复用 Ariel 蓝色气泡变量、圆角与间距，助理左侧沿用 Ariel 排版但保持 Puck 黑色主题中的无气泡正文；历史与完成态助理回复不显示头像，仅处理中状态行显示头像，最终回复落地后头像消失；多段回复为连续正文块；Markdown、代码、Mermaid、带标题链接渲染；时间戳不逐条常显，按 Asia/Shanghai 分组并可悬停／长按查看完整值（测试固定时钟与时区）。
- 即时确认：发送后 500 ms 内出现处理中指示；mock Brain 延迟 5 s 时 3 s 处出现模板确认，Brain 文本到达后不重复。
- 过程折叠：mock Brain 产生 20 个工具步骤时对话中只有一行“工作了 · 执行 20 步”，展开可见列表。
- 卡片：问题卡片单选／多选／自由输入／跳过；授权卡片批准、拒绝、过期变灰；任务卡片状态随事件原地更新；附件卡片打开现有查看器。
- 输入框与停止：工作中出现停止按钮；点击后进行中任务全部中断并出现“已停止”汇总消息。
- 输入与附件（第 17 轮增量，2026-11-05）：composer 左侧提供不小于现有触控目标的“附加截图”按钮，隐藏 input 仅接受 PNG／JPEG 且允许多选；复用 Ariel Codex 的 `readScreenshotFiles`／`ScreenshotDraft`，单次最多 3 张、解码后合计不超过 4 MiB，草稿显示可移除缩略图。纯文字、文字加图、纯图片均可发送，文字与图片都为空时禁用发送；成功发起后清空文字与图片草稿。composer 发送按钮仅在 Puck 内使用当前助理 `avatarDesign(selected).color`（`--puck-avatar-color`），换色后立即同步；箭头和 disabled 状态保持可读，其他 primary 按钮不继承头像色。
- 附件协议与持久化（第 17 轮增量，2026-11-05）：`puck.message.send` 与内部 `puck.brain.turn.start` 增加可选 `images: string[]`。每项必须是严格 base64 的 PNG／JPEG data URI，真实解码格式须与 MIME 一致，最多 3 张、解码后合计 ≤ 4 MiB，并沿用 Desktop Agent 的像素边界；`text` 可为空但 `images` 不可同时为空。用户消息正文仍只存文本，图片 data URI 以 `card_json.images` 持久化并由历史轮询／watch 回放为用户气泡缩略图，不写入 `text`。BrainHost 将文本项（非空时）与按序的 `{type:"image",url:dataUri}` 一起写入 app-server `turn/start.input`；无图片时 wire 与现有行为一致。任意非图片文件仍不支持。
- 错误恢复与触屏回应（第 17 轮增量追加，2026-11-05）：Desktop Agent 必须保留 `BRAIN_UNAVAILABLE` 等 Puck 明确错误码，不得把它们降级成 `RESYNC_REQUIRED`。`puck.brain.turn.start` 在明确 rejected 的 `BRAIN_UNAVAILABLE`／`RESYNC_REQUIRED` 下由 Module 重新 `puck.brain.ensure` 后只重试一次；read-only Brain 工具遇到 rejected `RESYNC_REQUIRED` 同参数重读一次，mutation／unknown 不自动重放。任何形如原始错误码的 assistant 文本或 Web error message 均映射为中文说明，不直接展示 code；本地失败气泡提供把原请求放回 composer 的“重试”入口。消息回应入口在桌面仅 hover／focus 显示；`hover:none` 下不常驻“+”，已有回应 pill 保留，长按消息打开表情选择器。
- 新建：表单提交后进入对话并收到问候与问题卡片；准备失败显示重试。
- 动态头像：十四种确定性形状与十一种明确颜色、每种形状适配的两个细长尖耳在侧栏、标题、消息与信息栏一致，并在 28–40 px 可辨识；任何状态都无白眼眶、叶冠、花朵、花瓣、嘴或眉毛；右侧选择器全集可点击、可键盘操作并立即持久化；两只黑色胶囊眼始终平行，整张脸八向移动并用近侧眼收窄表现透视，头部轮廓随之转向，随机双眼眨眼；全部实例共享单个 timeout 调度链，标签页隐藏或视口外暂停，动画不触发 React render，reduced-motion 下只保留低频眨眼。

### 1.3 Grok Bot 功能体验对照（第 10 轮）

本节是用户在 P0 已交付后批准的增量范围；仍须保持 `--enable-puck` 关闭时行为与资产字节不变。

| 项目 | 本轮决定 | 行为与边界 |
| --- | --- | --- |
| 主动问候 | 实现 | `puck.create` 完成 `puck.brain.ensure` 后立即启动一次真实 greeting turn，要求 Brain 简短介绍自己、能力边界并询问第一件事；Web 显示持久化的“正在准备”状态。若 thread 已建立但 greeting turn 无法启动，助理仍可创建，并落库一条明确的降级消息，不能伪造 Brain 文本。若 `ensure` 本身失败，维持既有 `provisioning` + 重试语义。 |
| 真实工作耗时 | 实现 | Relay 以 `puck.brain.turn.start` 被接受的时间为起点，以该 turn 的最终 agent message 落库为终点。进行中消息持久化 `startedAt`，Web 每秒仅更新可见计时文本；最终 `workSummary` 保存 `elapsedMs` 与该 turn 真实收到的 Puck tool call 数，显示“工作了 N 秒 · 执行 N 步”。不得用固定随机值或页面打开时间替代。 |
| 跨助理未读 | 实现（全局水位） | SQLite 为每个助理保存 `unread_count` 与最后消息预览；新的助理消息增加未读，`puck.list` 返回服务端值。打开会话后 Web 调用 `puck.read` 原子清零，所有 Web 端下一次同步看到一致结果。本轮采用账号级全局已读水位，不做“每台浏览器各自未读”，因为当前 Passkey 身份没有稳定的设备级 reader ID。 |
| 消息表情回应 | 实现（轻量） | 常用集合固定为 👍、❤️、😂、🎉、👀。用户经 `puck.message.react` 对任意消息回应；Brain 复用受信 `react` dynamic tool 对用户消息回应。每个 actor（`user`／`assistant`）在一条消息上最多一个 emoji，再点同一项取消；结果随消息落库并显示为小 pill。不做任意 emoji、表情线程或通知。 |
| 助理头像通知／图标 | 部分实现 | Puck Web 打开时，标签页 favicon 从当前助理实际 SVG 在浏览器本地栅格化为 PNG；切换助理即时更新，离开 Puck 后恢复 Ariel 图标。已安装 PWA 图标不能按当前助理动态改变；且 P0 尚未实现 §10 的 VAPID、Service Worker 和 Push dispatch，因此本轮不伪造 Web Push 图标能力，继续使用 Ariel 静态图标。P1 在通知 payload 携带无敏感信息的 avatar descriptor，由 Service Worker 选择或生成缓存 PNG；PWA 保留应用级 Ariel 图标。 |
| 可编辑说明 | 实现 | 信息栏提供显式“编辑说明／保存／取消”；保存复用 `puck.update.description`，SQLite 中 `description`／`persona` 同步更新。除创建 thread 时写入 `developerInstructions` 外，每个后续可信 Puck envelope 都重申当前长期说明，使不轮换的既有 brain thread 在下一 turn 生效；空说明使用明确的默认职责，不注入空白指令。 |

验收补充：Go 测试覆盖 schema v1→v2 迁移、问候成功／降级、耗时与步数、未读增减／清零、用户与 Brain 表情 actor 隔离、说明进入下一 turn；Vitest 覆盖实时秒表、工作摘要、未读切换、表情选择与说明保存；Playwright 覆盖 320／390／1280 px 的未读角标、表情 pill、说明编辑与动态 PNG favicon，且 Puck 退出后 favicon 恢复。

## 2. 已决事项（2026-11-03 用户决定）

| 编号 | 决定 | 落点 |
| --- | --- | --- |
| 命名 | 2026-11-03 22:43 用户将个人助理产品命名为 **Puck**（莎士比亚《仲夏夜之梦》中的精灵，与 Ariel 同属莎剧精灵，取其灵巧、跑腿的意象）。界面与文档中的产品／功能名一律为 “Puck”；用户创建的每个助理实例仍可有自己的名字，“助理”只作通用名词。代码与协议标识统一使用 puck：包 `internal/puck`、开关 `--enable-puck`／`ARIEL_PUCK_ENABLED`、`/api/puck/*`、Web `puck.*` 方法、Agent `puck.brain.*` 方法、能力 `puckBrain`／`puckBrainReady`、错误码 `PUCK_*`（如 `PUCK_REQUIRES_PASSKEY_HTTPS`、`PUCK_SELF_TARGET`）、数据目录 `/data/puck/`、overlay `passkey-puck`、brain thread 名 `[Puck] <名称>`、`ARIEL_PUCK_MEMORY_BUDGET`；实例级字段（`assistantId`、`assistant_id`、表 `assistants`）保留 assistant 作为通用名词 | 全文 |
| Q1 | 不要求每次派发都确认；助理默认自主派发／追加／停止，只有关键动作才找用户授权；Codex 原生审批与补充回答始终归用户 | §5、§6、§7.4、§9 |
| Q2 | 助理服务部署在 Relay 侧（服务端、与公开 Relay 一起部署；后经 Q10 修订为同一进程）；大脑跑在所选主机的 Codex 上，经现有 Relay／Desktop Agent 通道驱动；Brain 的工具调用也经 Relay 到达 Service；支持用户随时创建多个助理 | §3、§4、§7、§8 |
| Q4 | 通知先做 Web Push，其他渠道以后再说 | §10 |
| Q2 修订／Q10 | 不单独做 `cmd/ariel-puck` 进程；助理与 Ariel 是**同一服务（同一 Relay 二进制、同一进程）里的两个入口**，以降低运维负担；代码在 Ariel 仓库 `internal/puck`，配置开启、默认关闭 | §4、§7、§12、§14、§15 |
| Q20 | 接受 Puck 模块默认 128 MiB 独立内存预算；开启时上调 `GOMEMLIMIT` 与容器内存 | §4.1.1、§4.1.3、§15 |
| Q21 | Puck 数据与 Passkey 数据共用 `/data` 卷，放在独立子目录 `/data/puck/` | §4.1.1、§4.1.3、§8.1 |
| 体验 | 用户要求体验与界面尽量接近对话式个人助理，由设计方把控：新增 E1–E13 与 UI1–UI7，并写入各阶段验收 | §1.1、§1.2、§15 |
| Q3 | Brain 由 app-server 创建：`developerInstructions` 承载人设，策略按 thread／turn 设置（待写探针确认） | §4.2 |
| Q24 | brain thread 由大脑主机 Ariel Desktop Agent 自持的常驻 `codex app-server` 子进程长期持有，不交给 Codex Desktop，以 Puck 界面为主要入口（Desktop 侧边栏仍可见，Q27 接受）；执行 thread 仍是 Desktop thread | §4.2、§4.3、§6、§8 |
| Q13 | 工具采用 `dynamicTools`，由大脑主机 Agent 承接 `item/tool/call`，经既有 Agent↔Relay 连接交给 Puck 模块；主机上无 MCP 凭据、不依赖 Desktop 环境变量；不提供 `/api/puck/mcp` | §4.3、§6.1 |
| Q14 | 执行 thread 一律显式 `on-request` + `approvalsReviewer: user`；无法强制时拒绝自动派发并请用户授权；不改用户全局配置 | §4.4、§5.2 B10 |
| Q22 | v1 只在 Passkey + HTTPS 部署中提供 Puck；PIN 模式下 Ariel 照常工作但 Puck 不可用，`--enable-puck` 明确拒绝并说明原因 | §3.2、§4.1.4、§10、§15 |

## 3. 范围与非目标

### 3.1 v1 范围

- Puck 模块（`internal/puck`，编译进 Relay 二进制，`--enable-puck`／`ARIEL_PUCK_ENABLED=1` 开启，默认关闭；SQLite 持久化）：Puck CRUD、对话、任务记录与跟踪、关键动作授权、记忆、routine、Web Push。
- 第二入口：`/api/puck/*` HTTP（仅 Web 认证的少量端点，无 MCP 端点）、`/ws` 上的 `puck.*` 方法、Web 中的 Puck 入口；均与 Ariel 现有入口共用同一 HTTP server、同一端口、同一 Web 认证。
- Brain：每个助理一个 brain thread，由大脑主机 Desktop Agent 的常驻 `codex app-server` 子进程持有（Q24），Puck 模块经 Relay 进程内接口 → Agent `puck.brain.*` 驱动。
- Puck 工具：以 `dynamicTools` 声明在 brain thread 上，调用经 Agent↔Relay 连接交给模块执行（Q13，§4.3）。
- Relay 内部：为 Puck 模块提供进程内编排接口（§7.1）、来源标注、`userDecisionId` 门禁、brain thread 自引用拦截；Puck 未开启时 Relay 保持现有无状态行为。
- Desktop Agent：可选能力 `puckBrain`（默认关闭）：常驻 app-server 的启动、监督与重启，brain thread 驱动与工具调用中转，专用 `cwd` 目录管理；执行策略强制（§4.4）。
- Web：助理列表／创建／切换／设置、对话、授权与审批卡片、通知中心、Web Push 订阅与 Service Worker。

### 3.2 非目标（v1）

1. **Brain 自行批准 Codex 原生交互**：任何 `interaction.respond` 只能转交用户本人在卡片上做出的决定（`userDecisionId`）。
2. **多用户**：仍为单用户模型（`ariel-owner`）；“多个助理”是同一用户的多个助理，不是多租户。
3. **未安装 Codex Desktop 的主机**：作为执行主机不支持（执行 thread 必须是 Desktop thread）；作为大脑主机需要 Codex Desktop 应用包提供内置 codex 二进制，但不要求 Desktop 正在运行（§4.2.8）；纯 CLI 主机由 P4 另行规格化。
4. 不在 Relay 的会话热路径（`/ws` 路由、hot cache、Agent 转发）中引入数据库或持久化；持久状态只存在于开启后的 Puck 模块（§4.1）；不改变 Codex 为会话／执行 SSOT。
5. 不自动重放任何 `unknown` 结果的写操作。
6. 不恢复 0097 已撤回的 Web 任务看板；Puck 入口是对话形态。
7. 通知只做 Web Push；Telegram／飞书等以后另批。
8. 助理之间不互相驱动、不共享记忆或任务（§8.3）。
9. **PIN 模式或非 HTTPS 部署中的助理**（Q22）：这些部署中 Ariel 现有功能照常，Puck 不可用（§4.1.4）。

## 4. 架构

```mermaid
flowchart LR
  subgraph Proc["Relay 进程（同一二进制 / 同一容器）"]
    direction TB
    Core["Ariel Relay 核心（入口一）<br/>/ws 路由 · hot cache · 认证<br/>无持久状态"]
    Asst["Puck 模块 internal/puck（入口二，默认关闭）<br/>Puck · 工具执行 · 任务 · 授权 · 记忆 · routine · Web Push<br/>独立 goroutine supervisor · 有界队列"]
    DB[("SQLite<br/>Relay 卷 /data/puck/")]
    Core <-->|"进程内编排接口<br/>（有界、超时、recover）"| Asst
    Asst --- DB
  end
  Web["Ariel Web + Service Worker"] <-->|"/ws（含 puck.*）<br/>/api/puck/*（仅 Web 认证）"| Proc
  Asst -->|Web Push（VAPID）| PushSvc["浏览器推送服务"] --> Web
  subgraph BrainHost["大脑主机（例如 Mac mini）"]
    direction TB
    BAgent["Ariel Desktop Agent<br/>puckBrain：brain supervisor"]
    BAS["常驻 codex app-server（Agent 子进程）<br/>Desktop 内置 codex · stdio<br/>brain thread A、B…（dynamicTools）"]
    BAgent <-->|"stdio JSON-RPC<br/>turn/start · item/tool/call"| BAS
    BDesk["Codex Desktop（可选，仅执行工作需要）"]
  end
  Core <-->|"既有 agent 连接<br/>puck.brain.*（turn、事件、工具调用／结果）"| BAgent
  Core <-->|既有 agent 连接| ExecHost["执行主机 Desktop Agent<br/>Codex Desktop：执行 thread"]
```

数据流（概要；一条消息的完整时序见 §4.2.1）：

1. 用户在 Web 给助理 A 发消息 → Relay `/ws` 认证后把 `puck.message.send` 交给 Puck 模块（进程内调用）→ 模块记录消息并立即回显“处理中”→ 经编排接口向 A 的大脑主机 Agent 发 `puck.brain.turn.start` → Agent 在常驻 app-server 上对 A 的 brain thread 发 `turn/start`。
2. Brain 推理中调用 Puck 工具 → app-server 向 Agent 发 `item/tool/call`（动态工具，§4.3）→ Agent 经**既有 Agent↔Relay 连接**上送 `puck.brain.toolCall` 事件 → Relay 核心校验来源设备与 brain thread 后交给模块 → 模块按 A 的策略执行：非关键动作经编排接口对执行主机发 `thread.create`／`turn.start` 等；关键动作创建授权请求并推送 → 结果经 `puck.brain.toolResult` 下发 → Agent 回复 app-server。
3. 模块通过编排接口 watch 执行 thread，状态变化时更新任务、按需唤醒 Brain 写摘要，并发送 Web Push。
4. Brain 的回复由 Agent 以有界 `puck.brain.event` 上送（agent message 完成、步数统计、turn 结束），模块写入 A 的对话并推送到 Web。

### 4.1 放置决定：Relay 进程内的第二入口（Q10 已决）

用户决定（2026-11-03）：不做独立 `cmd/ariel-puck` 进程，助理与 Ariel 是同一服务里的两个入口，以降低运维负担。具体形态：

1. **代码**：Ariel 仓库 `internal/puck`（业务、存储、策略、Push、工具执行）＋ `cmd/relay` 中的装配；同一 `ariel-relay` 二进制、同一镜像、同一端口、同一 Web 认证（v1 仅 Passkey，见 §4.1.4）。
2. **开关**：`--enable-puck`／`ARIEL_PUCK_ENABLED=1`，配合 `ARIEL_PUCK_DATA_DIR`（默认 `/data/puck`，与 `ARIEL_WEBAUTHN_CREDENTIALS_FILE` 的 `/data/auth.json` 同卷，Q21）。默认关闭；关闭时不注册 `/api/puck/*` 路由（返回 404）、`puck.*` 方法返回 `FEATURE_DISABLED`、不创建数据文件、不启动任何 goroutine，Relay 行为与现状完全一致。
3. **第二入口**：`/api/puck/*` HTTP（Web Push 配置等，仅 Web 认证）、`/ws` 上的 `puck.*` 方法、Web 中的 Puck 入口；不新增端口、域名、容器或进程。
4. **去掉的设计**：原稿中的 `role=assistant` WebSocket 连接、服务 API key、内网来源限制和 `/api/puck/` 反向代理全部删除，改为进程内接口（§7.1）。`userDecisionId` 门禁和递归保护保留不变；原 MCP 凭据设计随 Q13 删除（§4.3）。

#### 4.1.1 “Relay 无状态”原则的变化

1. 原则修订为：**Relay 的会话热路径保持无状态**（`/ws` 路由、Agent 转发、hot cache 只在内存，Codex 仍是 SSOT），未开启 Puck 时整个进程仍无持久状态（Passkey 凭据文件 `/data/auth.json` 除外，与现状一致）。
2. 只有开启 Puck 后，进程才持有持久状态，且**只在 `internal/puck` 内部**：SQLite（`/data/puck/puck.db`，WAL；目录 0700，与 Passkey 的 `/data/auth.json` 共用卷但互不读取，Q21）、VAPID 私钥。Relay 核心包不得 import 助理存储，也不得读取其中数据（依赖方向：puck → relay 编排接口，反向只通过接口注入）。
3. Relay 的 hot cache 512 MiB soft budget 与 cache 计量不包含也不受助理影响；助理有独立的内存预算（默认 128 MiB，覆盖其队列、内存索引与 SQLite page cache），超限时丢弃／合并非关键事件并进入 `degraded`，不从 Relay 预算借用。由于同进程共享 `GOMEMLIMIT`，开启 Puck 的部署把 `GOMEMLIMIT` 与容器内存上限各上调助理预算（Q20 已决）：以现有 base Deployment 为例，`GOMEMLIMIT` 768 MiB → 896 MiB，`limits.memory` 1 GiB → 1152 MiB；未开启时保持原值。预算可通过 `ARIEL_PUCK_MEMORY_BUDGET` 调整，P1 长稳基准实测超出时先修实现或调参，不静默放宽。
4. SQLite 使用纯 Go 驱动（现有镜像 `CGO_ENABLED=0`，不引入 cgo），所有数据库访问在助理自己的 goroutine 中进行，Relay 请求处理 goroutine 不直接执行 SQL。

#### 4.1.2 故障隔离（同进程内）

1. **Supervisor**：Puck 模块的全部后台工作（watcher 消费、Brain 唤醒、routine scheduler、Push 发送、SQLite 写入）运行在模块自建的 goroutine 中，由 supervisor 统一启动；每个 goroutine 入口 `recover`，panic 记录脱敏堆栈后该子任务按指数退避重启；短时间内重复 panic（如 5 分钟 3 次）则整个模块熔断为 `PUCK_UNAVAILABLE`，Relay 核心继续服务。
2. **调用边界**：Relay → 助理的每次进程内调用（`puck.*` 方法、`/api/puck/*` handler、事件投递）都包一层 `recover` + 超时；Relay 侧 goroutine 永不阻塞等待助理：事件通过有界队列投递（满了按 0099／0100 的慢消费者规则对 Puck 订阅降级，只影响助理，不影响 Web 与 Agent）。
3. **助理 → Relay**：只能通过 §7.1 编排接口，受独立的并发／速率上限（写 120／分钟、watch 256、在途请求 32），用尽返回 `OVERLOADED`，不占用 Web 的配额。
4. **外部 I/O**：Push 外发 HTTPS、工具调用处理有各自的连接池、超时与并发上限；SQLite 损坏或磁盘满时模块进入 `degraded`／不可用，不影响 Relay 启动（开启 Puck 但数据目录不可写时，Relay 照常启动，助理报告不可用）。
5. **残余风险**：同进程意味着无法隔离内存耗尽、`fatal error`（如并发 map 写、栈溢出）、`os.Exit` 与死锁导致的整体故障；对策见 §12.10。

#### 4.1.3 部署影响

1. **Kubernetes**：现有 Deployment 已是 `replicas: 1` + `strategy: Recreate`，满足 SQLite 单写者要求，**无需新增 Deployment／Service／Ingress／PVC**。助理只在 Passkey overlay 上提供：复用其已挂载 `/data` 的 PVC（`ariel-auth`），数据放 `/data/puck/` 子目录（Q21）。新增一个基于 Passkey overlay 的 `passkey-puck` overlay（或在 Passkey overlay 上的可选 patch），设置 `ARIEL_PUCK_ENABLED=1`、`ARIEL_PUCK_DATA_DIR=/data/puck`，并把 `GOMEMLIMIT` 调为 896MiB、`limits.memory` 调为 1152Mi（Q20）。PIN overlay **不变**，保留 `emptyDir`，v1 不需要改为 PVC（Q22）。不允许扩到多副本（启动时用 SQLite 文件锁检测第二写者并拒绝开启 Puck）。PVC 备份同时覆盖 Passkey 凭据与 Puck 数据，备份与恢复步骤写入部署文档。
2. **Docker（`docs/operations/container-deployment.md`）**：同一镜像；仅 Passkey + HTTPS 部署可开启：加环境变量，复用已有 `/data` 挂载，并按 Q20 上调内存限制与 `GOMEMLIMIT`。
3. **ECS（`scripts/deploy-ecs.sh`）**：增加可选参数 `--enable-puck`，仅允许与 `--mode passkey`（Caddy HTTPS）一起使用；与 `--mode pin` 组合时脚本直接报错退出并说明原因，不改动现有部署。复用脚本已管理的持久目录（同一 `/data`），并按 Q20 上调容器内存与 `GOMEMLIMIT`；文档提示开启后实际内存以 P1 长稳基准为准。
4. **本机（`scripts/ariel.sh up local`）**：本机脚本使用 PIN 模式，v1 **不提供** Puck 开关；若环境中设置了 `ARIEL_PUCK_ENABLED`，按 §4.1.4 拒绝并给出原因。开发验证使用测试专用 Passkey + HTTPS harness（§15）。
5. **发布**：助理与 Relay 同版本同发布；升级、回滚与 Relay 一起进行。SQLite schema 迁移必须向前兼容至少一个版本（回滚到上一版本仍能启动，必要时上一版本以只读方式拒绝开启 Puck 而非破坏数据）。
6. **关闭时**：路由、方法、goroutine、文件、内存与现状完全一致，既有部署无需任何改动。

#### 4.1.4 开启前置条件与拒绝行为（Q22 已决）

1. **前置条件**：同时满足 `ARIEL_WEB_AUTH=passkey`，且 `ARIEL_PUBLIC_ORIGIN` 为 `https://` origin（Passkey 现有配置校验已要求这一点），且数据目录可写、未被其他进程锁定。
2. **不满足时的行为**：Relay **照常启动**，Ariel 现有功能完全不受影响；助理保持关闭，等同未开启：不注册 `/api/puck/*`（404）、不创建 `/data/puck/`、不启动 goroutine、不生成 VAPID 密钥。不把这种情况当作启动失败（避免因误配让整个 Ariel 不可用）。
3. **启动日志**：输出一条 WARN 级结构化日志，固定原因码，便于运维排查，例如 `puck disabled: reason=PUCK_REQUIRES_PASSKEY_HTTPS detail=pin_mode`；`detail` 取值 `pin_mode`、`public_origin_not_https`、`data_dir_unwritable`、`data_dir_locked`。日志不含凭据、路径以外的敏感信息。
4. **Web 呈现**：Relay 在既有 hello／auth status 能力中声明 `puck: {state: "off" | "unavailable" | "ready" | "degraded", reason?}`。`off`（未设置开关）时 Web 不显示 Puck 入口；`unavailable`（设置了开关但前置条件不满足或模块熔断）时显示禁用的 Puck 入口和说明，例如“Puck 仅支持 Passkey + HTTPS 部署，当前为 PIN 模式”；`puck.*` 方法返回 `FEATURE_DISABLED` 并带同一原因码。原因文案不得泄露服务器路径或配置值。
5. 前置条件只在启动时判定；运行期切换认证模式需重启 Relay，与现有行为一致。

### 4.2 Brain 的驱动方式（Q3、Q24 已决）

只读探针（2026-11-03 21:1x，用户 Mac mini）：Codex Desktop 为 `/Applications/ChatGPT.app`，内置 codex-cli 0.160.0（Ariel 使用内置版本；用户 `~/.local/bin/codex` 为 0.145.0-alpha.27，不使用）；`~/.codex/ipc/ipc.sock` 存在。app-server v2 schema：`ThreadStartParams` 含 `approvalPolicy`、`approvalsReviewer`、`baseInstructions`、`developerInstructions`、`config`、`cwd`、`ephemeral`、`model`、`modelProvider`、`personality`、`sandbox`、`serviceName`、`threadSource`、`sessionStartSource`；`TurnStartParams` 含 `approvalPolicy`、`approvalsReviewer`、`sandboxPolicy`、`cwd`、`effort`、`model`、`outputSchema`、`turnTrigger` 等；`ServerRequest` 含 `DynamicToolCall`。上游公开文档与源码（未在本机写探针）：`dynamicTools` 是 `thread/start` 的实验字段，需 `initialize.capabilities.experimentalApi = true`（Ariel 的 `InteractiveSession.Initialize` 已设置）；只能在 `thread/start` 时声明，`thread/resume`／`turn/start` 不能更新（openai/codex#24808），持久 thread 恢复时沿用已存工具；调用时依次出现 `item/started`（`dynamicToolCall`）→ `item/tool/call` 请求 → 客户端回复 `{contentItems, success}` → `item/completed`。

**决定（Q24，用户 2026-11-03）**：brain thread 由大脑主机上 Ariel Desktop Agent 自持的**常驻 `codex app-server` 子进程**长期持有，**不迁移给 Codex Desktop**；brain thread 以 Puck 界面为唯一交互入口；它仍会出现在 Desktop 侧边栏（写探针 + 用户目视确认，Q27 接受为已知外观行为，见 §4.2 第 11 条）。被派发的执行 thread 仍是 Desktop thread，路径与现状相同（§4.4）。

1. **进程**：Agent 能力 `puckBrain` 开启时，Agent 的 brain supervisor 启动**一个**常驻子进程 `<codex> app-server --listen stdio://`（复用 `appserver.StartInteractive`／`InteractiveSession`），该主机上的全部 brain thread 共用它。二进制选择与 Agent 现有逻辑一致（`probe.SelectBinary`：`ARIEL_CODEX_BINARY` 覆盖，否则取 Desktop 内置 codex，即 `/Applications/ChatGPT.app` 内的 CLI），并记录版本；低于验证过的最低版本时 `puckBrain` 报告不可用。进程使用用户现有 Codex 登录与 `CODEX_HOME`，Ariel 不读取、不复制任何凭据文件，也不改用户全局配置。
2. **工作目录**：每个助理一个专用空目录 `~/.ariel/puck/<assistantId>/`（0700，由 Agent 创建，只允许在该根下），作为 brain thread 的 `cwd`；不写 `AGENTS.md` 或 MCP 配置（Q15）。
3. **创建**：`thread/start {cwd, ephemeral:false, serviceName:"ariel-puck", threadSource:"chatgpt_hidden", developerInstructions, sandbox:"danger-full-access", approvalPolicy:"never", model?, dynamicTools:[§6.1 工具], config?}`。常驻进程以进程级参数关闭内置本地能力（写探针 D 组合，§4.2.2）：`--disable shell_tool unified_exec view_image image_generation goals browser_use browser_use_external computer_use in_app_browser sleep_tool multi_agent apps plugins tool_suggest skill_search`、`-c web_search="disabled"`、`-c include_apply_patch_tool=false`、用户全局 MCP 逐个 `enabled=false`；**不得关闭 `code_mode_host`**（0.160 中动态工具经它暴露）。启动子进程前清洗环境（丢弃非 UTF-8 值，探针中 `ARIEL_DEVICE_NAME` 导致 codex shell panic）。创建后立即 `thread/name/set {name:"[Puck] <助理名称>"}`（改名时同步）。`developerInstructions` = 人设 + 硬规则 + Desktop 误输入守卫（第 11 条）（E1、E3、E6、E9、E13 行为要求，禁止自批，只经工具行动，默认中文）。Brain 不执行本地写命令；`approvalPolicy` 取 `never`（无内置执行工具后无审批对象；动态工具不经审批，探针确认）（Q28 已决）。
4. **后续 turn**：`turn/start {threadId, clientUserMessageId, input, model?, effort?, approvalPolicy:"never", sandboxPolicy:{type:"dangerFullAccess"}}`（逐 turn 显式携带策略，因 sandbox 不随 thread 持久，见 §4.2.2） 直接发往常驻进程，**不经 Desktop IPC**；Agent 以 `turn/started` 与 exact `clientUserMessageId` 读回确认后向 Relay 回复 `accepted`，否则 `unknown`（不重放）。停止用 `turn/interrupt`（exact turnId）。
5. **工具集版本**：`dynamicTools` 只能在创建时声明，因此助理记录 `toolset_version`；工具集变更（新增／改 schema）通过**轮换 brain thread** 生效（与 #64 体积轮换同一机制：新建 thread、注入记忆与近期对话摘要、旧 thread 保留只读）。工具 handler 对未知或旧版本工具名返回结构化错误，不崩溃。
6. **监督与恢复**：子进程退出时 supervisor 指数退避重启（1 s 起，上限 60 s）；10 分钟内崩溃 5 次则 `puckBrain` 进入 `degraded` 并上报 Relay，停止重启直到 Agent 重启或用户在 Web 重试。重启后对已知 brain thread 惰性 `thread/resume`（下一次 turn 时），**必须显式携带 `sandbox:"danger-full-access"`、`approvalPolicy:"never"`**：裸 resume 会回落到全局 `danger-full-access` 并改写持久设置（探针 P1／P4）。进行中的 brain turn 随进程丢失：Agent 上报 `turn.lost`，模块把该轮标为中断，在对话中如实说明（“刚才中断了，我重新看一下”，E13），由用户消息或模块一次性重唤醒；已发出的工具写操作按幂等 ID 只读回、不重放。
7. **生命周期绑定**：常驻进程是 Agent 的子进程，随 Agent 退出而关闭（`InteractiveProcess.Close` 超时后 kill）；Agent 不在线时其上所有助理为 `brain_offline`（§8.5）。
8. **Desktop 是否必需**：brain 本身只需要大脑主机上**运行中的 Ariel Desktop Agent**、已安装的 Codex Desktop 应用包（提供内置 codex 二进制）与有效的 Codex 登录；**不需要 Codex Desktop 正在运行**。只有当该主机同时作为执行主机（被派发执行 thread）时才需要 Desktop 运行（`codexReady`）。
9. **与 #4（注册与就绪耦合）的关系**：当前 `desktopagent.Run` 在 Desktop IPC 健康检查（`checkDesktop`）通过前不会连 Relay，Desktop 未运行时整台主机显示离线，brain 也随之不可用。P0 对 `puckBrain` 做最小解耦：开启 `puckBrain` 时，Desktop 检查失败不阻止 Agent 上线，Agent 以 `codexReady=false`、`puckBrainReady=true` 注册（先例：`TasksManagerOnly` 已能以 `service == nil` 上线），Desktop 就绪后再按现有逻辑启用会话能力（或按 #4 的完整修复）。能力拆分为 `puckBrain`（已开启）与 `puckBrainReady`（常驻进程已初始化且版本合格），Web 与模块据此显示“大脑就绪／离线／降级”，不再把“Desktop 未开”误判为“大脑离线”。
10. **输出拆分**：每条完成的 agent message 成为一个对话气泡（UI2.2）；命令、动态工具调用与推理项只计入“工作了 N 秒 · 执行 M 步”详情（E3）。Agent 只上送有界事件（单条文本 ≤ 64 KiB、详情项摘要 ≤ 512 B），不上送完整 rollout。
11. **可见性**：brain thread 不进入 Ariel 现有会话视图：Agent 在 `thread.list` 结果中过滤自身 brain thread id，Relay 对 Web 发往 brain thread 的 `turn.start`／`thread.read` 等一律 `PUCK_SELF_TARGET`；Agent 拒绝对 brain thread id 打开 Desktop follower。brain thread 的 rollout 写入用户 `CODEX_HOME`，**会出现在 Desktop 侧边栏**（Q27 已决：v1 接受为已知外观行为）。仍设 `threadSource:"chatgpt_hidden"`（无害，Desktop 侧边栏代码已有排除逻辑，后续版本可能生效），并以「[Puck] <名称>」命名以便识别。**Desktop 误输入守卫**：（a）Agent 发出的每条 brain 输入都包裹在带本 brain 会话随机 nonce 的信封中（`<puck-input nonce=…>`，nonce 只存在于 Agent 内存与助理记录，不写入 `developerInstructions`）；`developerInstructions` 规定：最新用户消息不带有效信封时，不调用任何工具、不执行任何动作，只回复一句“这是 Puck 的内部会话，请在 Ariel 的 Puck 界面中与我对话”。（b）在 Desktop 中运行的 turn 拿不到动态工具（Desktop 不承接 `item/tool/call`），也没有执行主机路由，因此递归守卫（§4.3、§8.4）照常成立：Desktop 内的误输入无法派发任务或触发关键动作。（c）Agent 在每次 brain turn 前检查该 thread 的 rollout／`updated_at`；发现非本进程发起的 turn（turn id 不在 Agent 记录中）时，先以显式策略重新 `thread/resume` 同步历史，上报 `brain.foreign_turn` 审计事件，并在助理对话中提示一次“检测到有人在 Codex Desktop 中向助理会话输入了内容，已忽略”。（d）Desktop 加载该 thread 时可能把沙箱改回全局默认（§4.2.2 第 2 条）；常驻进程每个 turn 显式携带策略，不受影响。

#### 4.2.2 P0 写探针结果（2026-11-03，Mac mini，codex-cli 0.160.0）

详见 `docs/testing/0130-probe-results.md`。要点：

1. dynamicTools 往返通过；kill 进程后 `thread/resume` 工具仍可用（state DB `thread_dynamic_tools`）。
2. **sandbox 不随 thread 持久**：不带参数的 `thread/resume` 返回并写回全局 `danger-full-access`（仅 resume 即改写 state DB），`approvalPolicy` 则保留；`turn/start` 的 `sandboxPolicy` 生效并成为新的持久值。
3. 可见性：普通 thread 会被 Desktop 所用的 `thread/list` 列出；`threadSource:"chatgpt_hidden"` 已持久化、`thread/read`／`resume` 可读回，Desktop 侧边栏代码排除该值，但 `thread/list` 响应不带 `threadSource`（恒 null），UI 是否隐藏未目视确认；`ephemeral` 可隐藏但无法 resume，不可用；`serviceName`／`originator` 不参与 Desktop 过滤。与运行中 Desktop 共用 `CODEX_HOME` 未见锁冲突。 **22:06 用户目视确认**：带 `chatgpt_hidden` 的 thread 与对照 thread **都出现在** Desktop 侧边栏，即 0.160 下该值不能隐藏；Q27 按接受处理。
4. 内置能力可关闭至只剩动态工具（§4.2 第 3 条 D 组合，shell 请求得到 NO_SHELL、无 `commandExecution`）；关 `code_mode_host` 会令动态工具失效；残余面：code mode `exec`、`request_user_input`、模型自报的 `collaboration.*`（未实测能否 spawn）。
5. 执行 thread：`thread/start` 的 on-request + workspace-write 生效（cwd 内写成功、cwd 外被沙箱拒绝，模型未请求提权）；Desktop 26.930.51102／Codex 0.160.0 的 follower 显式 policy fixture 创建与 seed 通过，但 follower probe 返回 `IPC protocol incompatible`，未取得 `latestThreadSettings` 兼容读回，结论为 **unverified**。P0 按 §4.4 第 4 条／B10 fail closed，不自动派发。
6. 时延（热进程，trivial turn）：首个 delta 2543／2352／1545 ms，完成 2674／2928／1750 ms；含一次动态工具调用的 turn 约 12 s。

2026-11-11 在工作 Mac 上对 Desktop `26.1103.60933`／Codex CLI `0.167.0-alpha.3.2`（IPC profile `unverified`）补做生产 brain 路径隔离写探针，详见 `docs/testing/0130-probe-results.md` 同日条目。该精确组合与 Desktop `26.930.51102`／CLI `0.160.0` 一样允许承载 Puck brain；比较是完整版本字符串相等。相邻 Desktop build、相邻 CLI（例如 `0.167.0-alpha.3.1`）与 `unsupported` profile 仍关闭 brain。该探针不放宽 execution settings 或任务写门禁，Q23 follower 路径仍为 unverified。

#### 4.2.1 一条对话消息的时序

```mermaid
sequenceDiagram
  autonumber
  participant W as Web
  participant R as Relay 核心
  participant M as Puck 模块
  participant A as 大脑主机 Agent
  participant S as 常驻 app-server
  participant X as 执行主机 Agent／Desktop
  W->>R: puck.message.send(assistantId, clientMessageId, text)
  R->>M: 进程内调用（认证后）
  M-->>W: 消息已记录 + 处理中指示（≤ 500 ms，E1）
  M->>R: Forward(brainDevice, puck.brain.turn.start)
  R->>A: request
  A->>S: turn/start(threadId, clientUserMessageId, input)
  S-->>A: turn/started
  A-->>R: response accepted(turnId)
  S-->>A: agentMessage 完成（“好的，我先……”）
  A-->>R: event puck.brain.event(message)
  R->>M: 事件投递 → 对话气泡（≤ 2 s 目标；3 s 无文本则模板确认）
  S->>A: item/tool/call(callId, tool=start_task, args)
  A-->>R: event puck.brain.toolCall
  R->>M: 校验 deviceId＝brain 设备、threadId＝该助理 brain thread
  M->>M: 策略引擎（A 类／B 类）
  M->>R: Forward(execDevice, thread.create + turn.start, origin=puck)
  R->>X: request（on-request 策略，§4.4）
  X-->>R: accepted + exact clientMessageId 读回
  R-->>M: accepted
  M->>R: Forward(brainDevice, puck.brain.toolResult(callId, result))
  R->>A: request
  A->>S: 回复 item/tool/call {contentItems, success}
  S-->>A: 最终 agentMessage + turn/completed
  A-->>R: event puck.brain.event(message, turnCompleted, 步数)
  R->>M: 写入对话、任务卡片“已在执行主机开始”（E3）
  M-->>W: puck.* 事件推送
```

工具调用超时：模块对单次工具调用默认 60 s 内给出结果；需要用户授权的调用立即返回 `AUTHORIZATION_REQUIRED`（不挂起 turn）。Agent↔Relay 连接中断时，Agent 在 30 s 后以 `success:false`（“与 Ariel 服务器的连接中断，结果未知”）回复挂起的调用，模块在重连后按 `callId` 幂等记录读回，不重复执行。

### 4.3 工具接入：动态工具经 Agent 中转（Q13 已决）

**决定**：采用方案 B——Puck 工具以 `dynamicTools` 在 brain thread 创建时声明，由大脑主机 Agent 承接 `item/tool/call`，经**既有、已认证的 Agent↔Relay 连接**转交 Relay 进程内的 Puck 模块执行，结果原路返回。

1. **身份**：工具调用的身份由 Relay 核心根据**连接所认证的 `deviceId`** 与调用中的 `threadId` 映射得到 `assistantId`（模块维护 `(brain_device_id, brain_thread_id) → assistantId`）；Agent 上送的任何 `assistantId` 字段只作校验、不作信任依据。设备或 thread 不匹配一律拒绝并审计。
2. **无主机侧凭据**：不发放 MCP 凭据，不写任何主机配置文件，不依赖 Desktop 进程环境变量；大脑主机上不存在可用于调用 Puck 工具的独立密钥。Agent 自身的 device token 是唯一凭据，沿用现有管理。
3. **无新公网端点**：删除原 `/api/puck/mcp` 设计及其攻击面；`/api/puck/*` 只保留 Web 认证的少量端点（如 VAPID 公钥）。
4. **wire**：Relay→Agent 请求 `puck.brain.ensure`（建目录 + `thread/start` 或校验已存在）、`puck.brain.turn.start`、`puck.brain.turn.interrupt`、`puck.brain.toolResult`、`puck.brain.status`、`puck.brain.retire`（轮换／删除时停止使用某 thread，不删除 rollout）；Agent→Relay 事件 `puck.brain.event`、`puck.brain.toolCall`、`puck.brain.status`。这些方法只接受来自 Puck 模块（`Origin.kind=puck`）的请求，Web 客户端调用一律拒绝；未开启 `puckBrain` 的 Agent 返回 `FEATURE_DISABLED`。schema 以可选扩展方式加入（0109），旧 Agent 不受影响。
5. **有界**：单次 `arguments` ≤ 64 KiB、结果 ≤ 64 KiB（截断并标 `truncated`）；每 brain thread 同时在途工具调用 ≤ 4；Agent 端挂起调用表有上限，超出直接回复 `success:false`。
6. **执行 thread 永不声明 `dynamicTools`**：执行 thread 经 Desktop 路径创建（§4.4），Agent 的执行 bootstrap 代码路径不接受 `dynamicTools` 参数（测试断言）。
7. **备选（不实现）**：若未来 Codex 版本移除或改变实验性 `dynamicTools`，备选是每 thread `config.mcp_servers` 注入的 HTTP MCP（`codex mcp add --url` 已证实支持 Streamable HTTP，但凭据只能经 Codex 进程环境变量或配置内静态头送达）或 stdio shim 经 Agent 中转；届时另立规格评估，v1 不保留 MCP 端点。

### 4.4 执行 thread 的审批策略（Q14 已决）

探针：用户全局 `~/.codex/config.toml` 为 `approval_policy = "never"`、`sandbox_mode = "danger-full-access"`、`approvals_reviewer = "user"`。若派发时继承这些设置，A 类原生审批**永远不会出现**，§5 的执行门禁失效。修改用户全局配置不在范围内。

**现有写路径能力（代码核对，`internal/codex/desktopipc`，2026-11-03）**：

1. 生产路径 `StartProductionTurnImages`（`production.go`，由 `follower.go` 调用）发出的 `thread-follower-start-turn` 只带 `turnStart.request = {threadId, clientUserMessageId, input}` 与 `context.inheritThreadSettings: true`，**不带** `approvalPolicy`／`sandboxPolicy`。
2. 同一包内的 fixture 路径在**同一个** `turnStart.request` 对象中携带逐 turn 策略，且仍设 `inheritThreadSettings: true`：`approval.go` 与 `pending_probe.go`（`approvalPolicy: "on-request"`、`sandboxPolicy: {type: "readOnly"}`）、`input_exercise.go`（`never` + `readOnly` + Plan 模式）。即协议形状可承载逐 turn 策略。
3. 真实证据有限：`docs/compatibility/2026-10-03-m0.md` 记录 read-only／on-request fixture 触发了真实 `item/commandExecution/requestApproval`；但 `docs/testing/2026-10-04-m1-m4.md` 观察到以 read-only／on-request 创建的 thread 在 Desktop 自动加载后 sandbox 变为 `danger-full-access`（审批仍为 on-request），且当时全局配置未知。2026-11-03 app-server 写探针（§4.2.2）已在 0.160.0、全局 `never`／`danger-full-access` 下确认：app-server `turn/start` 的逐 turn `sandboxPolicy` 生效并写回持久设置；裸 resume 会把 sandbox 回落为全局值（解释了 m1-m4 的现象），审批策略保留。Desktop 26.930.51102／Codex 0.160.0 的 follower 显式 policy 探针返回 `IPC protocol incompatible`，无法取得兼容读回；Q23 因而保持 **unverified**，实现必须走第 4 条 B10 fail closed。
4. 新 thread 路径：`AppServerBootstrapper.BootstrapWithSettings`（`internal/desktopagent/appserver_bootstrap.go`）的 `thread/start` 当前只传 `cwd`／`ephemeral`／`serviceName`／`model`／`serviceTier`／`config.model_reasoning_effort`，首个 `turn/start` 只传模型参数；可扩展加入 `approvalPolicy`／`approvalsReviewer`／`sandbox`。`appserver.ThreadCreator.Create` 只传 `cwd`。代码中未使用 `approvalsReviewer`。
5. 生效设置可读回：owner 状态的 `latestThreadSettings` 含 `approvalPolicy`／`sandboxPolicy`（见 `testfixture/large_owner.go`；生产给 Web 的元数据只截取模型字段）。

**决定**：

1. 助理派发的执行 thread 一律显式设置 `approvalPolicy: "on-request"`、`approvalsReviewer: "user"`，sandbox 默认 `workspace-write`（turn 级 `sandboxPolicy` 取对应的 workspace-write 类型，字段名以 schema 为准；限定在任务 `cwd`；用户可按设备／目录在“主机与权限”中调整，但不能设为 `never` 审批）。
2. 新建执行 thread：经 Agent 的 app-server 首轮路径在 `thread/start` 与首个 `turn/start` 上同时携带上述策略（Agent 新增能力声明 `executionPolicy`，旧 Agent 不声明则视为不可强制）。
3. 续投 turn：follower `thread-follower-start-turn` 在 `turnStart.request` 中显式携带同样策略（扩展生产路径，形状同 fixture 路径）。
4. 每次派发／续投前后读回 owner 的 `latestThreadSettings`：派发前若无法确认可强制（旧 Agent、读不到设置、写探针未通过的 Codex 版本），**拒绝自动派发**，返回 `EXECUTION_POLICY_UNENFORCEABLE`，并在对话中发授权卡片说明“该主机此会话将以无审批方式运行（命令不会再找你确认）”，用户可“批准一次”（规则 B10）或取消；派发后读回发现策略被改回 `never` 时，任务进入 `attention` 并通知用户，不再自动续投。
5. 不修改用户全局 `~/.codex/config.toml`，也不改动用户自己发起的 thread 的设置；B2 场景（续投用户自己的 thread）同样走第 4 条检查。

## 5. 关键动作与授权（Q1 已决）

### 5.1 原则

助理默认**自主执行**，不逐次确认。以下两类属于关键动作，必须由用户授权：

- **A 类：Codex 原生交互（固定，不可关闭）**。执行 thread 中出现的 `command_approval`、`file_approval`、`permission_request`、`user_input`（补充提问）以及 `unsupported` 交互，**一律**推送给用户，由用户在现有审批／回答卡片上决定。Brain 只能附加背景说明和建议，不能代为决定；Relay 对 puck 来源的 `interaction.respond` 强制要求 `userDecisionId`（§7.4）。
- **B 类：敏感动作策略（每个助理可配置，默认全部开启）**。由 Service 的策略引擎在执行前判定；命中即转为授权请求。

### 5.2 B 类默认清单

| 规则 | 判定依据（结构化） | 默认 |
| --- | --- | --- |
| B1 越界作用域 | 目标 `deviceId` 不在该助理允许设备内，或 `cwd` 不在该设备允许根目录内 | 需授权 |
| B2 操作非本助理创建的 thread | `send_turn`／`interrupt` 目标不是该助理任务表中的执行 thread（例如用户自己的会话） | 需授权 |
| B3 停止用户发起的 turn | 目标 turn 的 `origin` 不是本助理 | 需授权 |
| B4 会话管理类写入 | 归档、重命名、置顶、删除队列项、编辑已发送消息等（v1 工具未开放；若后续开放，默认需授权） | 需授权 |
| B5 声明的高风险意图 | Brain 必须在 `start_task`／`send_turn` 上声明 `riskTags`；含 `push`、`deploy`、`release`、`delete`、`destructive`、`credential`、`payment`、`external_message` 任一标签 | 需授权 |
| B6 内容兜底 | Service 对 prompt 做关键词／模式检查（如 `git push`、`--force`、`rm -rf`、`kubectl apply`、`deploy`、`DROP TABLE`），命中但 Brain 未声明 `riskTags` 时按 B5 处理并记录“声明不一致” | 需授权 |
| B7 事件唤醒中的自我扩展 | Brain 在“任务事件／routine 唤醒”（非用户消息驱动）的 turn 中创建、修改或删除 routine、修改策略或允许作用域、派发新任务 | 需授权（派发可按 routine 预授权范围放行，见 §11） |
| B8 资源上限 | 超过该助理的并发执行 thread 上限（默认 3）或每小时派发上限（默认 10） | 需授权 |
| B9 记忆删除／批量覆盖 | 删除记忆或单次覆盖 > 5 条 | 需授权 |
| B10 无审批执行 | 目标执行 thread 的审批策略无法强制为 `on-request`，或读回为 `never`（§4.4） | 需授权，不可关闭 |

说明与边界：

1. B5／B6 依赖 Brain 自述与文本检查，**只能降低风险、不能保证拦截**；真正的执行门禁仍是执行主机 Codex 自身的沙箱与审批（A 类）。若执行主机的 Codex 配置为 `never` 审批或完全放开沙箱，A 类审批不会出现，Ariel 无法替代。为此派发时一律显式要求 `approvalPolicy: "on-request"`，无法强制时命中 B10（§4.4，Q14 已决）。
2. 用户可以按助理关闭 B5–B9 中的规则或调整阈值；B1–B3 可以通过扩大允许作用域来减少命中，但规则本身不可关闭；B10 不可关闭；A 类不可关闭。策略变更本身只能由用户在 Web 设置中完成，Brain 发起的策略变更一律命中 B7。

### 5.3 授权流程

1. 命中关键动作时，Service 冻结完整动作（工具名、参数、目标设备／thread、prompt 摘要、`riskTags`、命中规则），计算 `actionHash`，生成 `authorizationId`（单次、默认 30 分钟过期），向 Brain 返回 `AUTHORIZATION_REQUIRED`（含 `authorizationId`），并推送“需要授权”通知。
2. 用户在 Web 授权卡片上看到命中规则、目标、完整 prompt，选择“批准一次”或“拒绝”；批准由用户 Web 会话发送 `puck.authorization.decide`，Service 校验 `actionHash` 未变后**执行冻结的原动作**，结果作为事件写回对话并唤醒 Brain。Brain 不能修改已冻结动作，只能重新提交新动作（会产生新的授权）。
3. 授权与 A 类 `userDecisionId` 都由用户 Web 会话产生；Brain 与工具调用都无法创建、批准或延长授权。
4. 过期、拒绝、目标已变化（例如 thread 已归档）均使动作终止，任务进入 `attention`。
5. 授权以对话内卡片呈现（E5、UI3.2），Push 深链到同一卡片；卡片原地更新为“已批准／已拒绝／已过期”。

## 6. 组件与职责

| 组件 | 职责 | 不负责 |
| --- | --- | --- |
| Puck 模块（Relay 进程内） | Puck CRUD；对话（即时“处理中”指示与模板确认 E1、里程碑消息 E3、停止 E12）；经 Agent 驱动 brain thread；执行 Brain 的工具调用；策略引擎与授权；任务记录与 watcher；记忆；routine；Web Push；递归保护；审计 | 成为执行会话 SSOT；保存完整 transcript；替用户审批 |
| Brain（每助理一个，常驻 app-server 中的 thread） | 理解请求、规划、选择主机／目录、调用工具、撰写摘要 | 持有任何凭据；执行本地写命令；批准任何交互或授权 |
| Puck 工具（`dynamicTools`） | 工具 schema（随 `toolset_version`）、参数校验、结果截断、错误码 | 绕过 Service 策略 |
| Relay 核心 | Web 认证后把 `puck.*`、`/api/puck/*` 交给模块；提供进程内编排接口；按 Agent 连接 `deviceId` 把 `puck.brain.*` 事件交给模块；来源标注；`userDecisionId` 门禁；brain thread 自引用拦截；调用边界的超时与 recover | 读取或持久化任何 Puck 数据；为助理阻塞热路径 |
| Desktop Agent（大脑主机） | `puckBrain`：常驻 app-server 生命周期（二进制选择＝Desktop 内置 codex、监督／重启、崩溃熔断）、专用 `cwd`、brain turn 驱动、`item/tool/call` 中转、有界事件上送、brain thread 在会话列表与 follower 中的隔离；Desktop 未运行时仍以 `puckBrainReady` 上线 | 判断动作风险；执行工具；持有工具凭据 |
| Desktop Agent（执行主机） | 现有能力 + 执行策略强制（`executionPolicy`，§4.4） | 判断动作风险 |
| Web | 按 §1.2 实现三栏布局、对话气泡、对话内卡片、输入框与停止、新建流程、信息栏设置；通知中心；Web Push 订阅与 Service Worker | 调用 `puck.brain.*`；绕过 `puck.*` 直接操作模块数据 |

### 6.1 Puck 工具（`dynamicTools`）

所有工具按调用者 `assistantId`（由 Relay 依据 `(deviceId, brainThreadId)` 映射，§4.3）隔离，返回有界 JSON（单次 ≤ 64 KiB，摘要截断并标注 `truncated`），错误码沿用 `protocol/v1.schema.json` 的 `error.code`，新增码见 §7.5。

| 工具 | 输入 | 行为 | 映射／判定 |
| --- | --- | --- | --- |
| `list_devices` | — | 本助理允许范围内的设备、在线、`codexReady`、能力 | `device.list` + 作用域过滤 |
| `list_threads` | `deviceId`, `search?`, `limit≤50` | 会话元数据；brain thread 不出现 | `thread.list` |
| `read_thread` | `deviceId`, `threadId`, `turns≤4` | 最近 turn 有界摘要、runtime、pendingInteractions | `thread.cache.read` 优先，回退 `thread.read`；越界读取需授权（B1） |
| `start_task` | `deviceId`, `cwd`, `goal`, `prompt`, `riskTags[]`, `clientTaskId`；可选 `includeMessages[]`、`referenceThreads[]`（§6.1.4） | 新建执行 thread 并发首个 turn，写任务记录。若本助理已有同一设备与目录的可复用线程，且用户没有明确要求新会话，则改为续投（§6.1.1）。真正发出的首条输入由服务端附上交接包 | 策略引擎；新建时放行后原子 `thread.bootstrap`（0134 取代旧 `thread.create` + `turn.start` 映射）；续投走 `send_turn` 的同一门禁 |
| `send_turn` | `taskId` 或 (`deviceId`,`threadId`), `text`, `riskTags[]`, `clientMessageId`；可选 `includeMessages[]`、`referenceThreads[]`（§6.1.4） | 续投同一执行 thread；同一设备与目录的后续请求默认走这条路径。真正发出的输入由服务端附上交接包 | 策略引擎（B2／B5／B6／B10）→ `turn.start` |
| `interrupt` | `taskId`／`threadId`, `turnId`, `reason` | 精确停止 | 策略引擎（B2／B3）→ `turn.interrupt` |
| `respond_interaction` | `interactionId`, `userDecisionId` | 只转交用户已作出且未消费的决定 | 无 `userDecisionId` 必拒 |
| `get_task_status` | `taskId?` | 本助理的任务记录 + 最新 runtime／交互摘要。先读 Relay 热 cache，不新鲜时回退 `thread.read`；读不到现场时 `runtime=unknown`，不得把库里的旧 `running` 说成已完成 | Service 本地 cache，回退 `thread.read` |
| `watch_thread` | `deviceId`, `threadId`, `until?` | 只读监听一条本助理没有启动的现有会话。`until` 是用户要等的结果。进度和重复状态不发消息；明确结果、失败、卡住或停止才通知并取消监听。不调用 `turn.start`。成功返回 `watching:true` 之后才能向用户承诺主动通知 | 既有 B2／`PUCK_SELF_TARGET` 仍拒绝；不授予执行权 |
| `finish_task` | `taskId`, `outcome`（`completed`／`failed`／`cancelled`）, `note?` | 归档本助理自己的一条任务，关闭该执行线程上本助理的 watch，并删掉尚未投递的唤醒。不删除对话消息，不发送 turn | 只接受调用者自己的任务；brain thread 返回 `PUCK_SELF_TARGET`；不经过 `turn.start`，因此不能绕过 B2／B5／B6／B10／B8 |
| `send_message` | `text`（1–4000 字）, `clientMessageId?` | 把一句 Bot 决定要说的话写进本助理的 Ariel 对话，并标成可通知的 `notify_user`。同一 `clientMessageId` 只保留一条 | 只写调用者自己的对话；参数里的 `assistantId` 拒绝；不能改别人的消息。工具集版本 9 起声明 |
| `request_authorization` | `summary`, `action` | Brain 主动就某事请用户授权（例如“是否部署”） | 生成授权卡片 |
| `memory_read`／`memory_write`／`memory_delete` | — | 本助理记忆 | 删除命中 B9 |
| `routine_list`／`routine_upsert`／`routine_delete` | — | 本助理 routine；创建／修改后在对话中显示例行卡片（E8） | 事件唤醒中命中 B7 |
| `ask_user` | `question`, `options[]`（`label`, `style: primary/danger/default`）, `multiSelect?`, `allowFreeText` | 在对话中发问题卡片（E6）；回答作为下一条用户消息唤醒 Brain | 一次只允许一个未答问题卡片 |
| `share_result` | `items[]`（`kind: diff/file/pr/thread/url`, `title`, `deviceId?`, `threadId?`, `path?`, `url?`） | 在对话中发附件卡片（E11）；文件与 diff 只存引用，点开时经 Relay 现场读取 | 越界引用拒绝（B1） |
| `react` | `messageId`, `emoji`（白名单） | 对用户消息加表情回应（UI2.3） | 仅限本助理对话中的用户消息 |

### 6.1.1 执行线程复用（2026-11-11）

同一 Bot 的执行线程持有工作上下文。默认不要为每一条后续请求再开一条 Codex 线程，否则 Desktop 侧边栏会被占满，上下文也会丢掉。

**规则**：用户向同一个 Bot 发出的后续请求，如果目标设备与目录和该 Bot 自己的某条已有执行线程相同，就续投那条线程（`send_turn`）。只有下面三种情况才 `start_task` 新建：

1. 没有可复用的已有任务：设备不同、目录不同，或旧线程已经不存在／不可用；
2. 用户明确要求单独的新会话（例如「新会话」「独立任务」「new session」「separate thread」；紧挨着的否定，如「不要新会话」「don't start a new session」，不算要求新建）；
3. 同一条用户消息上的既有幂等与改目录纠偏（0137）优先于跨消息复用。这条消息如果已经为它新建过任务，重复 `start_task` 仍返回该任务；同消息把目录改到别处时，仍按 0137 把这条消息绑定的任务改到新目录。跨消息续投上去的旧线程不会被这次纠偏覆盖：目录一旦不同，旧线程保持原绑定，新目录另建任务。

**Brain 可见上下文**：每次可信 envelope 附上该 Bot 最近的可复用任务，最多 8 条，含 `taskId`、`deviceId`、`cwd`、`goal`、`status`、最近活动时间，并写明优先 `send_turn`。引用任务卡时，原有「优先 `send_turn(taskId)`」说明仍然有效。envelope 是提示；服务端在 `start_task` 上再做一次导流，避免旧 brain 指令仍习惯性新建。

**服务端导流**：

- 只在本助理自己的任务表里按 `deviceId` + 规范化 `cwd` 选择最近一条可复用任务。别的助理的线程不会被选中（B2，v1 直接拒绝，不借授权跳过）。
- 可续投：`running`、`review`、`done`、`attention`、`stopped`，以及仍带 native thread、且不属于下面阻塞或不可用集合的状态。`attention` 上的用户跟进视为「用户指示」，可以续投，但 B10 等既有门禁照常执行。
- 正忙或正等用户，不打断、不排队、不另开线程：`dispatching`、`waiting_user`、`awaiting_authorization`、`reconciling`、`stopping` 直接返回已有 `TURN_BUSY` 和 `taskId`。`running` 仍尝试 `turn.start`；执行主机若返回 `TURN_BUSY`，原样交回 Brain，同样不另开线程。Q9 未决，服务端不新增队列。
- 不可用：`failed_dispatch`、`cancelled`、没有 native thread id，或续投得到 `NOT_FOUND`。`NOT_FOUND` 把旧任务标为 `cancelled` 后再新建。其他失败原样返回，不改成第二条线程。
- 续投成功后仍用原来的 `taskId`，并把这条用户消息记到该任务上，使同消息重试不再次 `turn.start`。
- 已冻结并获批准的 `start_task` 按冻结动作执行，不在批准后改判成续投。需要授权的续投冻结为 `send_turn`。

**仍然生效的门禁**：B2（只续投本助理任务表中的线程）、B5／B6、B10（执行策略无法强制时仍要授权，不自动派发）、B8（续投不新建执行线程，因此不占用新的并发线程名额；每小时派发上限只计实际新建）、`PUCK_SELF_TARGET`（不得把 brain 工作区或 brain thread 当成执行目标）。导流发生在这些检查之前或之中，不能绕过它们。

**兼容**：不新增 `start_task`／`send_turn` 必填字段，不改变旧 Agent／旧 Web 的既有帧（0109）。结果里多出的 `reused` 等字段可被旧客户端忽略。显式新会话由用户原文和本次 `goal`／`prompt` 识别，不依赖新的工具参数，因此尚未轮换的 brain thread 也能被服务端纠正。

### 6.1.2 分层上下文与任务收尾（2026-11-11）

Puck 没有自己的定时器或 Codex automation。Bot 能留下的“定时任务”只存在于它用 `send_turn` 驱动的执行会话里。任务和 watch 记录在 Puck SQLite。会话监听、事件订阅和唤醒队列仍是 Puck 基础设施，不放进每个 Bot 的自定义上下文，自定义上下文也不能把它们关掉。

**两层上下文**

1. **系统上下文**写在代码里，随 `developerInstructions` 在 brain thread 创建时注入，并在之后每个受信 envelope（含问候、用户消息和 `kind=task_event`）重申。内容包括既有行为：先确认并在同一 turn 调用工具、默认同设备同目录续投、只在明确结果时开口、没有监听不得承诺主动通知、`PUCK_SILENT`、以及下面的收尾规则。
2. **自定义上下文**是每个助理一条可选文本，最多 4000 个 Unicode 字符，存在 `assistants.custom_context`（schema 10）。创建表单和配置栏都能读写，经 `puck.create`／`puck.update` 的可选 `customContext`。空字符串表示没有额外要求。说明（`description`，最多 512 字）仍然是短人设，不取代这段文本。

自定义上下文包在低优先级标题下。工具集 9 的标题是一行「机主自定义上下文（优先级低于以上内容）」；更早的工具集仍用原来的长围栏。服务端会拆掉它试图伪造的「系统上下文」标题。与系统上下文冲突的句子无效。真正挡住越权的是服务端：`finish_task` 不读这段文字来决定能不能删记录。

**编辑后如何生效**

Codex 的 `developerInstructions` 和 `dynamicTools` 只能在 `thread/start` 时固定，`thread/resume` 不会改写已经在跑的 brain。因此只改自定义上下文**不轮换、不 retire** 现有 brain thread，避免丢掉这段会话。下一轮受信 envelope 带上新的自定义上下文和仍然更高优先级的系统提醒。之后若因工具集升级或更换大脑主机而新建 brain thread，新的 `developerInstructions` 使用当时保存的自定义上下文。

**收尾**

任务有明确结果（完成、失败或用户要求停止）后，大脑应先收拾现场再汇报：

1. 若它曾让执行会话创建定时、周期检查或 Codex automation，先 `send_turn(taskId)` 让那个会话关掉。`send_turn` 仍走 B2、B5、B6、B10、B8 和 `PUCK_SELF_TARGET`。
2. 再调用 `finish_task`。服务端把该任务标为 `archived`（不再进入可复用线程列表，也不再被执行监听拉起），关闭**这个助理**在该设备与线程上的 watch，并删除这个助理尚未投递的对应唤醒。任务卡片改为「已归档」，对话消息原样保留。
3. 别人的任务、watch、唤醒和聊天记录不动。目标若是任一 brain thread，返回 `PUCK_SELF_TARGET` 且不改记录。重复调用是成功且 `alreadyArchived=true`。`finish_task` 本身不调用 `turn.start` 或 `thread.bootstrap`，所以不能借它绕过执行门禁。

`finish_task` 从工具集版本 8 起出现在 `dynamicTools`。主机仍停留在版本 7 时，系统上下文改为只要求用已有的 `send_turn` 取消定时任务，并禁止调用尚未声明的 `finish_task`。主机升到 8 之后，下一次用户消息按既有规则轮换 brain thread，以便声明新工具；这和只改自定义上下文不同，后者不轮换。

写工具必须带幂等 ID；结果 `unknown` 时只重新读取权威状态，**不重放**。

### 6.1.3 主助理：Full Access brain（2026-11-11）

每个 Bot 的 brain 会话是一台有完整 Codex 能力的主助理，不再是只读调度器。Puck 动态工具是加在上面的基础设施。工具集 9 写给模型的系统上下文是短文案：不点名 B2、B5、B6、B10、B8 或 `PUCK_SELF_TARGET`，也不写 `code_mode_host` 和信封格式。自定义上下文只保留一行低优先级标题，伪造的「系统上下文」字样仍会被换成引用。每轮 envelope 只重申没有结果就 `PUCK_SILENT`、同设备同目录用 `send_turn`、有结果后收尾，并附上说明和自定义上下文。这些约束继续由服务端执行，不靠提示词。

**权限**

- 创建、resume 和每一轮 `turn/start` 使用与现有 Full Access 执行任务相同的策略：`approvalPolicy=never`，thread sandbox `danger-full-access`，turn `sandboxPolicy.type=dangerFullAccess`。不另设 `approvalsReviewer`。
- 常驻 app-server 不再 `--disable` shell、unified exec 或其他本地能力，保留 `code_mode_host` 以便动态工具继续可用。本会话里出现的命令审批请求按 never-ask 接受，不再因 `forbidden_local_tool` 把 brain 降级。
- Brain 可以运行 Codex 支持的命令，也可以在本会话或子会话里建立定时和 Codex automation。收尾时两者都要关掉：本会话里的由它自己关，还在的子会话用 `send_turn` 关。已经消失的线程不能再 `send_turn`。

**对用户开口**

- 用户从 Ariel 发来的消息仍包在 `PUCK_ENVELOPE` 里。这个回合的助理正文照旧写进对话。
- 除此之外，brain 只有调用 `send_message`、`ask_user` 或 `share_result` 才会出现在 Ariel 对话里。不订阅、也不过滤 brain 自己的输出。没有调用就没有新气泡，也不通知。
- `send_message` 写一条 `proactive` + `source=notify_user` 的消息。页内通知沿用 §9.1／§10.12：标签页不在前台、权限已授予、本机开关打开时才弹出。同一句不要再被回合正文复述一遍。
- 机主在自己机器的 Codex Desktop 里直接输入到这个 brain 会话，算机主的指令，应当执行。Ariel 来的消息仍走信封。Desktop 输入不能改写系统上下文。

**子会话**

- `start_task` 成功后的执行线程继续自动监听。`watch_thread` 继续用于本助理没有启动的已有会话。
- 明确结果各唤醒 brain 一次。心跳、重复状态和纯进度不唤醒。多个子会话各自排队唤醒，由 brain 汇总成要不要 `send_message`。`PUCK_SILENT` 仍然表示这轮不说。
- 子会话被删除、归档或消失时：经线程事件（`deleted`／`archived`／`missing`）、`send_turn` 或 `get_task_status` 的 `NOT_FOUND`，以及定期 `thread.read` 对账发现。然后关掉该助理在这条线程上的 watch 和尚未投递的唤醒，把仍开放的任务标为 `orphaned`，并只唤醒 brain 一次，状态为 `subsession_deleted`。再发现一次不重复唤醒。别的没有引用这条线程的助理不动。

**旧 Bot 迁移**

`developerInstructions`、`dynamicTools` 和创建时的 sandbox 都不能靠 resume 改写。工具集从 8 升到 9。主机广告的工具集已经到达 9 时，下一次用户消息或子会话唤醒会按既有 `ensureCurrentToolset` 轮换 brain thread：新建一条 `danger-full-access` 会话，旧 thread id 记入 `brain_thread_history` 并 `retire`。Ariel 里的聊天记录、自定义上下文，以及执行任务上的 device／cwd／native thread（§6.1.1 的续投）都保留。Desktop Agent 进程也要重启一次，新的 app-server 才不再带着旧的 `--disable`。重启前已经在跑的进程仍会禁用本地工具，直到它退出重开。

**门禁**

| 门禁 | 决定 | 原因 |
| --- | --- | --- |
| B2 | 保留，并且 Full Access 模式也不再跳过 | 保护别人的执行线程。brain 在本机 Full Access 并不等于可以向任意 thread `send_turn` |
| B5／B6 | ask 模式保留；Bot 自己的 `permissionMode=full` 仍自动放行其派出的任务 | 这是 0135 已有的 Full Access 行为，管的是派到其他设备的任务，不管 brain 自己的本地命令 |
| B8 | 保留为派发资源上限，不因 brain Full Access 放宽 | 规格里的并发／每小时上限。当前代码还没有单独的 B8 规则，这次不新增、也不把它说成已经失效 |
| B10 | ask 模式在执行策略无法强制时仍要授权；full 模式继续不因此拦自己的派发 | B10 防止无法强制审批的目标被自动派发。brain 自己的 Full Access 就是 `danger-full-access`，不走这条派发检查 |
| `PUCK_SELF_TARGET` | 保留 | `start_task`／`send_turn` 不能指向 brain 工作区或 brain thread |
| Passkey、仅机主设备 | 保留 | 不新增入口，不把工具调用变成跨用户通道 |

里程碑消息（E3）由模块根据 §9 状态迁移生成固定模板的任务卡片更新，不依赖 Brain 叙述；Brain 只在需要时追加一条简短解读。记忆写入成功后模块在对话中显示“已记住：……”小标签（可撤销，E7）。

### 6.1.4 交接包（2026-11-11）

Brain 用 `start_task` 或 `send_turn` 派活时，服务端不摘抄用户原话，也不总结被引用会话。摘要是模型自己的事。交接包只附会话引用，由服务端在通过既有门禁、真正调用 `thread.bootstrap` 或 `turn.start` 时加在 Brain 指令后面。Brain 的 `prompt`／`text` 原样保留。没有引用时不附加包。

引用来自本助理自己的 `tasks` 和 `thread_watches`（含已结束的 watch），并且只包括这一轮点了名的会话：Brain 指令、触发这轮的用户消息、它所回复的消息、`includeMessages` 里的消息提到了 `taskId`／`threadId`，或者 `referenceThreads` 明确列出了它们。别的助理的线程、brain thread（含历史 brain thread）和这次发送的目标线程都不进入包。`includeMessages` 只用来发现会话，不把消息正文贴进去。最多 4 条，多出来的直接丢掉，不做文字截断，也不写省略标记。

每条引用含 `deviceId`、`threadId`，以及 Codex Desktop 的 `codex://threads/<uuid>`（`internal/desktopagent/load.go`，0074／0004；非 UUID 不写链接）。标题和 cwd 只在已有元数据时附上，不从对话里摘结论。cwd 按 `session_meta.cwd`、任务记录的 cwd、turn cwd 的顺序选取；`thread.cwd` 只有在它不是 Agent 状态目录时才用。`~/.ariel` 和 `/agent-managed/` 一律不用。不写 watch 状态、turn 状态或结论摘录。

可选参数从工具集 10 起出现在 schema 里，都不是必填。旧调用不带它们仍然有效。主机广告的工具集到达 10 后，下一次用户消息或子会话唤醒经既有 `ensureCurrentToolset` 轮换 brain thread。工具集 10 的系统上下文只多一行：交接时 Puck 会附上被引用会话的链接；需要时自己去读，并写下你的目标和判断。这一行不写入每轮 envelope。

包在发送前现装，不写入冻结的授权参数。同一次 `send_turn` 正文里如果已经有交接标记，不再附第二份。同一用户消息对同一目标的重复 `start_task` 仍按 0137 返回已有任务，不会再发一轮。B2、B5、B6、B10、B8 和 `PUCK_SELF_TARGET` 仍先于交接包执行。

## 7. Relay 内的入口与门禁

### 7.1 进程内编排接口（取代 `role=assistant` 连接）

1. Relay 核心在 `internal/relay` 暴露窄接口（示意）：`Devices()`、`Forward(ctx, Origin, deviceID, method, payload) Response`、`Watch(ctx, Origin, threadKey) (<-chan Event, cancel)`、`DeviceStatus() <-chan Event`、`BrainThreads(set)`、`BrainEvents() <-chan BrainEvent`（Agent 上送的 `puck.brain.*` 事件，附连接认证的 `deviceId`）。实现复用现有 `forwardRequest`、路由表与 watcher，助理被视为一个**进程内虚拟客户端**（固定 `clientKind=puck`），不经过 WebSocket、不需要 API key、不占用 Web 连接数。
2. `Origin` 由模块按调用来源填写：`{kind:"puck", assistantId, trigger: user_message|tool|routine|event}`；Relay 据此做来源标注与门禁，不信任 Brain 传入的任何身份字段。
3. 方法白名单（最小权限）：`device.list`、`thread.list`、`thread.read`、`thread.history`（有界）、`thread.cache.read/watch/unwatch/promote/touch`、`thread.bootstrap`、`turn.start`、`turn.interrupt`、`interaction.respond`（门禁见 §7.4），以及仅对 brain 设备开放的 `puck.brain.*`（§4.3）。`thread.create` 仅保留旧内部兼容，不再用于 `start_task`。其他方法（`auth.pair.*`、`thread.archive.*`、`thread.name.update`、`thread.pin.update`、`queue.*`、`file.context.*`、`workspace.*`、编辑与设置写入）在接口层直接拒绝。
4. 独立配额：写 120／分钟、watch 256、在途 32；用尽 `OVERLOADED`，不影响 Web／Agent。
5. 各助理的设备／目录作用域在模块内执行；接口层只做模块级白名单与 brain thread 拦截（第二道防线）。

### 7.2 事件订阅

1. 模块通过 `DeviceStatus()` 接收 `device.status`。
2. `Watch` 复用 0100 watcher 引用计数，与 Web 共享同一 Agent follower，**不新增 per-client native follower**。
3. 事件经有界队列交给模块；队列满按慢消费者规则对模块订阅降级（模块随后重新 `Watch` 并重读权威状态），Relay 不等待。
4. 模块熔断或关闭时释放全部 watch；恢复后按任务表与 brain thread 列表重新 watch。

### 7.3 `puck.*` 方法与 `/api/puck/*`

`/ws` 新增：`puck.list/create/update/delete`、`puck.message.send`、`puck.stop`（E12：中断该助理当前 brain turn 与全部进行中任务的 exact turn，返回逐项结果 `stopped／not_running／unknown`，并在对话中发汇总消息）、`puck.question.answer`、`puck.history`、`puck.watch/unwatch`、`puck.task.list`、`puck.authorization.list/decide`、`puck.decision.record`、`puck.memory.list/update/delete`、`puck.routine.list/upsert/delete`、`puck.push.config/subscribe/update/unsubscribe/test`、`puck.notifications.list/ack`。Relay 先按现有规则认证 Web session，再在进程内交给模块；模块未开启返回 `FEATURE_DISABLED`，开启但熔断／不可用返回 `PUCK_UNAVAILABLE`。`/api/puck/*` HTTP 只含少量无须 WebSocket 的端点（如 VAPID 公钥），全部要求 Web 认证；不提供 MCP 端点。schema 采用可选扩展协商，旧 Web／旧 Agent 不受影响（0109）。

### 7.4 用户决定门禁与来源标注

1. 对 `Origin.kind=puck` 的 `interaction.respond`，Relay 核心在编排接口内要求 `userDecisionId`：由 Relay 核心（不是 Puck 模块）在用户 Web 会话调用 `puck.decision.record` 时签发并保存在内存，绑定 `(interactionId, decision, answers 摘要)`、单次有效、10 分钟过期；否则 `FEATURE_DISABLED`。v1 推荐路径仍是用户在 Web 卡片上直接走现有 `interaction.respond`；门禁用于“Push 通知 → 卡片 → 由模块转交”等间接路径。即使在同一进程内，模块也拿不到签发能力，只能引用 Relay 签发过的 ID；进程重启后未消费的 ID 全部失效。
2. 来自编排接口的 `thread.create`／`turn.start`／`turn.interrupt` 由 Relay 附加 `origin:{kind:"puck", assistantId}`，Agent 只记录不改变行为；Web 在执行 thread 中标注“由 <助理名称> 派发”。
3. 所有经编排接口的写请求沿用 `accepted/rejected/unknown/not_submitted`；`unknown` 不自动重放。

### 7.5 新增错误码

`PUCK_UNAVAILABLE`、`PUCK_ASSISTANT_NOT_FOUND`、`EXECUTION_POLICY_UNENFORCEABLE`、`PUCK_SELF_TARGET`、`AUTHORIZATION_REQUIRED`、`AUTHORIZATION_EXPIRED`、`BRAIN_HOST_OFFLINE`、`BRAIN_UNAVAILABLE`（大脑主机在线但常驻进程未就绪／降级）、`PATH_NOT_ALLOWED`（若现有 schema 已有同义码则复用，不新增）。

## 8. 多助理

### 8.1 数据模型（SQLite，位于 Passkey 共用数据卷 `/data/puck/puck.db`）

所有业务表带 `assistant_id` 外键，查询一律按 `assistant_id` 过滤；跨助理查询只允许在 Service 内部维护任务（如递归保护）中使用。

| 表 | 关键字段 |
| --- | --- |
| `assistants` | id（不可变 ULID）、name、avatar、description、persona（≤ 16 KiB）、brain_device_id、brain_cwd、brain_model?、brain_reasoning?、brain_thread_id、brain_thread_history（已轮换的旧 thread）、toolset_version、allowed_scope（设备 → 允许根目录）、policy（B 类开关与阈值）、limits、status（provisioning／ready／brain_offline／disabled／deleted）、created_at、updated_at、deleted_at |
| `conversation_messages` | id、assistant_id、role（user／assistant／event）、kind（text／question／authorization／task／attachment／approval／notice）、text（≤ 64 KiB）、card_json（≤ 8 KiB，卡片结构化数据）、reaction、work_summary（耗时、步数）、brain_turn_id、created_at |
| `tasks` | task_id、assistant_id、device_id、cwd、native_thread_id、goal、status、risk_tags、created_by、last_turn_id、last_seen_revision、last_summary（≤ 4 KiB） |
| `task_events` | task_id、assistant_id、kind、native_turn_id、interaction_id、at、dedupe_key |
| `authorizations` | id、assistant_id、action_json、action_hash、rules_hit、status、expires_at、decided_at |
| `pending_decisions` | decision_id、assistant_id、interaction_id、device_id、thread_id、decision、answers_digest、expires_at、consumed_at |
| `memories` | id、assistant_id、scope（global／project／device）、key、text、source、updated_at |
| `routines` | id、assistant_id、schedule（cron／RRULE）、timezone、prompt、preauthorized_scope、enabled、last_run_at、next_run_at |
| `push_subscriptions` | id、endpoint_hash、endpoint（加密存储）、p256dh、auth、label（浏览器／设备名）、user_agent_family、created_at、last_success_at、failure_count |
| `notification_prefs` | subscription_id、assistant_id、enabled、kinds（completed／failed／approval／authorization／question／offline／routine） |
| `notifications` | id、assistant_id、task_id、kind、payload_digest、dedupe_key、sent_at、status |
| `audit_log` | id、assistant_id、actor（user／brain／routine／service）、action、target、result、at（不含正文） |

不保存执行 thread 完整 transcript、工具原文、文件内容或图片；原文按需经 Relay 现场读取。复用 0091／0095／0096 的思想（领取 → WIP → 续投同一 thread、`agent_host`＝稳定 `deviceId`、`agent_id`＝native `threadId`、durable journal 防重复创建），但 v1 不依赖 Chilling 或 Obsidian。

### 8.2 生命周期（CRUD）

1. **创建**（`puck.create`）：输入名称、人设、大脑主机（设备 + 可选模型）、允许作用域、策略。Service：
   1. 校验大脑主机在线且 `puckBrainReady`（不要求 `codexReady`）；
   2. 发 `puck.brain.ensure`：Agent 在固定根目录（`~/.ariel/puck/<assistantId>/`）创建空的专用 `cwd`（根目录外拒绝，不写入文件），在常驻 app-server 上 `thread/start`（`developerInstructions`、`dynamicTools`、策略），返回 `brain_thread_id`；
   3. 发问候首轮（UI5.2），以 exact `clientUserMessageId` 读回确认；
   4. 状态 `provisioning → ready`；任一步失败停在 `provisioning` 并可重试；重试复用已记录的 `brain_thread_id`，不重复建 thread。
2. **列表／读取**（`puck.list`）：名称、状态、大脑主机在线、未读与待授权数量，并返回 `maxActiveAssistants`，供 Web 显示服务端权威容量。
3. **更新**（`puck.update`）：名称、人设、作用域、策略、通知设置即时生效；人设变化在下一 turn 以“设置已更新”系统前缀告知 Brain；人设大幅变更或工具集升级时轮换 brain thread（`developerInstructions`／`dynamicTools` 只能在创建时设定，§4.2.5）；**更换大脑主机**会在新主机新建 brain thread，旧 brain thread 由原主机 Agent `retire`（保留 rollout，不再驱动），记忆／任务／routine 不变。
4. **删除**（`puck.delete`）：二次确认。同一次 SQLite 事务里：把助理标成 `deleted`，拒绝待处理授权并删除持久授权规则，删除该助理的 `tasks`、`thread_watches`、`brain_wakes`、`wake_log`、`group_members` 和侧栏 `assistant` 放置，清空 `custom_context`、`brain_thread_id`、`brain_thread_history`，并把 `toolset_version` 置 0。第二次删除返回 `PUCK_ASSISTANT_NOT_FOUND`，不再改数据。**聊天记录沿用既有软删除**：`conversation_messages` 留在库里，但接口因助理已删除而不可读；不在删除时物理擦除。不删除其他助理的任何行。不删除磁盘上的 Codex rollout：只对 brain thread 调用既有 `puck.brain.retire`（本进程不再驱动），不发 `turn.interrupt`，也不删除执行线程文件。执行线程因此停止被这个 Bot 跟踪，但 Codex 会话本身还在。
5. 活跃助理数量上限默认 50（防止误建过多 brain thread）。创建时必须在同一 SQLite 事务内统计 `deleted_at IS NULL` 的助理并插入新记录，避免并发请求越过上限；达到上限返回 `OVERLOADED`。Web 显示当前数量与服务端上限，并在达到上限时禁用新建入口。软删除后的助理不占活跃容量。

### 8.3 隔离

1. 每个助理的对话、记忆、routine、任务、授权、通知设置互相独立；工具调用经 `(deviceId, brainThreadId)` 映射只能访问自己的数据。
2. 助理不能读取、续投或停止另一个助理创建的执行 thread（视为 B2；v1 直接拒绝，不提供授权跳过）。
3. 一个助理的故障（大脑主机离线、brain thread 超限）不影响其他助理；模块级故障（熔断）影响全部助理，Web 显示“Puck 不可用”，但 Ariel 现有会话功能不受影响（§4.1.2）。

### 8.4 递归保护（按助理）

1. 模块维护全局 brain thread 集合（所有助理，含已删除但未归档者）。任何助理的 `start_task`／`send_turn`／`interrupt`／`respond_interaction`／`read_thread` 目标若是任一 brain thread，拒绝 `PUCK_SELF_TARGET`；Relay 核心编排接口对 `Origin.kind=puck` 的请求做同样的检查（模块通过 `BrainThreads(set)` 同步集合）作为第二道防线。
2. 派发 `cwd` 不得位于 `~/.ariel/puck/` 根目录内（任何助理的工作区），避免执行 thread 与 brain 工作区混淆。
3. 执行 thread 不声明任何 `dynamicTools`、不注入凭据；工具只存在于常驻 app-server 中的 brain thread（§4.3.6）。brain thread 不在 Desktop 中持有，Agent 拒绝为 brain thread id 打开 Desktop follower，Relay 拒绝 Web 对 brain thread 的任何读写（第三道防线）。
3a. 工具调用来源校验：`puck.brain.toolCall` 的 `deviceId` 必须是该助理的 brain 设备，`threadId` 必须是其当前 brain thread；已 retire 的 thread 发起的调用一律拒绝。
4. 派发深度 1：任务事件唤醒的 turn 中再次派发属于 B7。
5. 唤醒频率上限：每助理每任务每 5 分钟 1 次，每助理全局每分钟 6 次；超出合并到下一窗口。

### 8.5 多个助理共享同一大脑主机

1. 允许；每个助理拥有独立 `cwd` 与 brain thread，共用该主机的同一个常驻 app-server 进程（进程故障影响该主机上全部助理，§4.2.6）。
2. 同一大脑主机上同时运行的 brain turn 数默认上限 2，其余唤醒在 Service 内排队（不使用 Codex 原生 queue，避免与用户手动输入混在一起）。
3. 大脑主机离线（Agent 不在线）或常驻进程降级（`BRAIN_UNAVAILABLE`）时，其上所有助理进入 `brain_offline`：用户消息照常记录并提示“大脑离线，恢复后处理”；任务 watch、状态机与 Web Push **继续工作**（模块在服务器侧 Relay 进程内），只是完成摘要降级为模板文本，恢复后补写一条汇总。
4. 一个助理的大脑主机也可以是执行主机；执行 thread 与 brain thread 通过 §8.4 区分。

## 9. 任务状态机

```text
(用户消息) → acknowledged（≤ 500 ms 处理中指示；3 s 无 Brain 文本补模板确认，E1）→ brain turn
(Brain 调用 start_task)
  ├─ 策略未命中 ──────────────→ dispatching
  └─ 命中关键动作 ─→ awaiting_authorization ─(用户批准，actionHash 一致)→ dispatching
                                          └─(拒绝／过期／目标变化)→ cancelled
dispatching ─(thread.create + turn.start accepted，且读回 exact clientMessageId)→ running
dispatching ─(rejected／not_submitted)→ failed_dispatch
dispatching ─(unknown)→ reconciling ─(读回存在)→ running ｜ ─(读回不存在)→ failed_dispatch（不自动重发）
running ─(出现 A 类原生交互)→ waiting_user ─(交互 resolved)→ running
running ─(send_turn／interrupt 命中关键动作)→ awaiting_authorization ─(批准)→ running ｜ ─(拒绝)→ running（动作不执行）
running ─(turn completed)→ review（摘要；native turn 成功 ≠ 任务完成）
review ─(Brain 自主追加 send_turn，未命中关键动作)→ running
review ─(Brain 判断达成目标)→ done（用户可在卡片上“重新打开”）
running／waiting_user ─(failed／interrupted／thread.error／设备离线 ≥ 2 分钟)→ attention
attention ─(Brain 自主恢复或用户指示)→ running ｜ cancelled
(brain turn 进行中) ─(常驻进程退出)→ 该轮标为中断，对话中如实说明；已发出的工具写操作只读回，不重放
任一非终态 ─(用户停止 puck.stop，E12)→ stopping ─(exact interrupt 确认)→ cancelled ｜ ─(unknown)→ reconciling
dispatching ─(执行策略无法强制，B10)→ awaiting_authorization
```

1. 一个任务绑定唯一 `(deviceId, nativeThreadId)`，续投永远发同一 thread（0096）。同一 Bot 对同一设备与目录的后续请求默认续投已有任务，而不是再绑定一条新线程（§6.1.1）。
2. 删除了强制的 `proposed` 确认态（Q1）；只有命中关键动作才进入 `awaiting_authorization`。
3. `review → done` 默认由 Brain 判定、可被用户重新打开（Q8 仍开放，见 §13）。
4. 设备离线时任务保持原状态并标注离线，不自动换机。
5. Brain 的自主追加有上限：同一任务连续自动追加默认 3 次后转 `attention`，防止无限“继续”。
6. **里程碑映射（E3、E4、E13）**：`running`（首次）→“已在 <主机> 开始”；`waiting_user`／`awaiting_authorization`／`attention`／设备离线 →“被阻塞：<原因>”+ 卡片 + Push；`review`／`done`／`failed_dispatch`／`cancelled` → 结果消息 + Push。其余迁移只更新任务卡片，不发新消息。任务卡片的状态标签只取自本状态机；`reconciling` 显示“状态待确认”；只有 `done` 显示“已完成”。

### 9.1 任务监听与主动唤醒（2026-11-11 批准的 P1 切片）

2026-11-11 的一次真实使用里，Bot 答应“做完告诉你”，执行线程其实已经结束，Puck 却要等用户再问才知道。核对代码后的原因是：Desktop Agent 只把常驻 app-server 上的 brain thread 事件上送为 `puck.brain.event`；执行线程的状态在 Relay 的 thread cache 里，Puck 没有订阅。`get_task_status` 只读 SQLite，而状态几乎只在创建时写成 `running`。大脑指令又写了“用 get_task_status 跟进”，没有禁止在没有监听时承诺主动通知。

本切片按上面的原因补上，不把整份 P1（审批卡、通知中心、按类型开关、头像 PNG、B7–B9、brain 轮换）一次做完。

1. **监听什么**：本助理 `tasks` 里仍可跟踪的执行线程（有 native thread，且状态不是 `done`／`cancelled`／`failed_dispatch`／`stopped`），以及 `thread_watches` 里状态为 `watching` 的只读监听。订阅走 `PuckBridge.Watch`，与 Web 共用同一个 Agent follower，不另开 native follower。
2. **何时唤醒**：只在结果发生有意义的变化时唤醒。明确结果包括：这一轮给出了新的完成结论（`review`）、失败（`attention`）、需要审批或补充输入（`waiting_user`）、已停止（`stopped`），以及摘要满足本次监听的 `until`（例如“发布完成”或“卡住了”）。同一种状态重复出现、摘要没有变化、以及只是进度或心跳的 turn 结束（“还在检查”“没有变化”）不唤醒，任务保持进行中并继续听。长跑或定时汇报的线程因此不会每个 turn 都发一条聊天。唤醒信封是受信的 `kind=task_event`，只含 task id、新状态、最后一条助理消息的短摘要（折叠空白，最多 240 字）。不把交互 prompt、命令或完整 thread id 写进日志。
2a. **沉默**：服务端先合并、去重，再决定是否唤醒。被唤醒后，大脑如果判断没有新的话要说，最终回复只写 `PUCK_SILENT`，并且不调用 `send_message`。这条约不入库、不出现在对话里，也不触发浏览器通知。工具集 9 起，唤醒回合的助理正文本身不进对话；要让用户看见，必须调用 `send_message`（§6.1.3）。多个子会话各自唤醒，由大脑汇总。浏览器通知只对着这种用户可见的消息。
3. **大脑忙或超限**：同一助理已有 brain turn 时，唤醒写入 `brain_wakes`，等当前 turn 结束后再发，不丢。同一任务 5 分钟内只交付一次，同一助理每分钟最多 6 次；多出来的合并进下一次窗口，保留最新状态和摘要。大脑离线时改写一条模板消息，仍然让用户看见。
4. **重启**：监听对象就是 SQLite 里的任务行和 `thread_watches`。进程重启后按这些行重新 `Watch`，并用 `thread.read` 对账；Agent 重新上线时只重连该设备上的监听。不重放写操作。
5. **只读监听**：`watch_thread` 给大脑一个办法盯住用户点名的、不是自己启动的会话，可选 `until` 写明要等到的结果。brain thread 仍是 `PUCK_SELF_TARGET`；别的助理的执行线程仍拒绝。监听期间和取消之后，对这条线程的 `send_turn` 都返回 `WATCH_ONLY`，除非它本来就是本助理自己的任务。B5／B6／B10／B8 的执行门禁不因监听而放宽。只读监听在出现明确结果后取消；进度汇报不取消、也不发消息。
6. **对用户说话**：只有 `start_task` 成功或 `watch_thread` 返回 `watching:true` 之后，大脑才能答应“做完告诉你”。否则必须说明只能在用户再问时汇报。这句话写在 `developerInstructions` 里，也写进之后的每个受信 envelope，这样还没轮换的旧 brain thread 下一轮就能看到。`watch_thread` 的 `until` 使工具集版本变为 7；主机跟上之后，下一次用户消息会按既有规则轮换 brain thread。旧线程靠 envelope 里的 `PUCK_SILENT` 说明也能保持沉默。
7. **页内通知**：任务事件落成用户可见消息后，如果本机已经在信息栏打开通知、浏览器权限是 granted、并且页面不在前台，Web 调用 Notification API；Service Worker 已注册时改由它 `showNotification`。`/sw.js` 不拦截 fetch。信息栏的开关在用户点击后才申请权限。

## 10. Web Push 通知（Q4 已决）

1. **VAPID**：Service 首次启动生成 P-256 VAPID 密钥对；私钥存 `/data/puck/vapid_private.pem`（0600，不入日志），公钥通过 `puck.push.config` 提供；`subject` 为部署配置的 `mailto:` 或站点 URL。轮换会使所有订阅失效，需在 Web 提示重新订阅。
2. **Service Worker**：Web 新增 `/sw.js`（根作用域，由 Relay 静态资源提供，`Cache-Control: no-cache`），**只处理 `push` 与 `notificationclick`**，v1 不做离线缓存或请求拦截，避免影响现有加载与升级；`notificationclick` 聚焦已打开的 Ariel 窗口或打开 `/?puck=<id>&task=<id>` 或授权卡片深链，导航后由 Web 正常认证。
3. **订阅**：用户在“通知”标签页或对话中的通知权限卡片（UI3.6）上显式点击“开启通知”（用户手势内调用 `Notification.requestPermission`）；`PushManager.subscribe({userVisibleOnly:true, applicationServerKey})` 结果经 `puck.push.subscribe` 存入 `push_subscriptions`，按浏览器／设备一条，附用户可编辑标签。只有已认证 Web 会话能增删订阅；订阅生命周期独立于 Web session（Passkey session 过期不自动取消订阅，退出登录时提供“同时取消本设备通知”选项）。
4. **按助理与类型开关**：`notification_prefs` 以（订阅 × 助理）为粒度，类型包括完成、失败、需要审批、需要授权、需要回答、设备离线、routine 结果；默认开启审批／授权／回答／失败，完成与 routine 默认开启，离线默认关闭。
5. **载荷与隐私**：按 RFC 8291 `aes128gcm` 加密（推送服务看不到明文）；明文 JSON ≤ 2 KiB（远低于 4 KiB 上限），只含：`assistantName`、`kind`、任务标题（≤ 60 字符，Brain 生成或 goal 截断）、设备名、状态、深链 ID、`tag`（用于合并同一任务通知）。**不含** prompt 原文、代码、文件路径、命令内容或审批详情；审批／授权细节只在点击后的已认证页面展示。可选“通知中显示摘要”按助理开启，开启后加入 ≤ 120 字符摘要，并在设置中提示锁屏可见风险。
6. **投递**：`TTL` 审批／授权／回答 24 小时，其余 4 小时；`Urgency` 审批／授权／回答为 `high`，其余 `normal`；同一 `dedupe_key` 只发一次；streaming 期间不发进度；每助理每分钟上限 6 条，超出合并为一条汇总。推送服务返回 404／410 立即删除订阅；连续 5 次其他失败暂停该订阅并在 Web 提示。
7. **iOS／iPadOS**：需 iOS／iPadOS 16.4+，且必须“添加到主屏幕”并从主屏幕图标打开后才能申请通知权限；Web 在 iOS Safari 非独立模式下显示添加到主屏幕的引导，不展示无效的开启按钮。现有 `manifest.webmanifest` 已声明 `display: "standalone"`，可直接满足主屏幕 PWA 要求，改动时须保持 `brand-assets.test.ts`／`public-positioning.test.ts` 通过。
8. **HTTPS**：Web Push 与 Service Worker 只在 HTTPS 可用；v1 Puck 本身只在 Passkey + HTTPS 部署中开启（§4.1.4），因此不存在“Puck 可用但不能推送”的部署形态。
9. 页面打开且正在查看对应助理时，Service 仍发送推送，由 Service Worker 在有可见客户端时改为页面内提示（`clients.matchAll` 判断），避免重复打扰。
10. 通知中心：已读水位存 Service（不是浏览器本地 unread），多设备同步。
11. **2026-11-11 切片没有做完的部分**：VAPID 密钥生成与持久化、RFC 8291 载荷加密、`push_subscriptions`／`notification_prefs`／`notifications`、向浏览器推送服务投递，以及标签页完全关闭后的到达。`/sw.js` 已经能处理 `push`、`notificationclick` 和页面转发的 `puck-notify`，但没有订阅，也没有服务端投递，所以关页之后目前不会响。页内、后台标签的系统通知见 §9.1。通知中心、按助理／类型开关、头像 PNG 和 iOS 主屏幕引导仍留在本节的后续 P1。
12. **`send_message` 的页内通知**：Bot 用 `send_message` 写下的消息带 `proactive:true` 与 `source:"notify_user"`。Web 用和任务结果相同的条件决定要不要弹出系统通知：本机开关打开、浏览器权限是 granted、页面不在前台。同一条消息只通知一次。用户信封回合里的普通回复不带这个标记，因此不会因为一句回答再弹一次。大脑离线、唤醒发不出去时，仍按 §9.1 写一条模板，避免结果彻底消失；这是唯一不经过工具调用的兜底。

## 11. Routines（P2）

1. routine = `(schedule, timezone, prompt, preauthorized_scope)`；cron 或 RRULE，时区默认用户时区（Asia/Shanghai）。
2. 触发时 Service 以 routine 前缀消息唤醒该助理的 Brain；`preauthorized_scope`（创建时由用户确认的设备／目录／`riskTags` 白名单）内的派发不再授权，范围外按 §5 处理。
3. 大脑主机离线、Relay 重启或模块熔断期间错过的触发只补跑最近一次。
4. routine 的创建／修改／删除：用户在对话中直接要求时 Brain 可执行（用户消息驱动的 turn）；事件／routine 唤醒中执行命中 B7。

## 12. 依赖、风险与约束

1. **#4 注册与就绪耦合**：Agent 在 Desktop／App Server 就绪前不上线，会被误判为离线。对大脑主机的影响：Desktop 未运行即“大脑离线”。P0 对 `puckBrain` 做最小解耦（§4.2.9：Desktop 检查失败仍以 `puckBrainReady` 上线）；执行主机侧仍受 #4 影响，离线需持续 ≥ 2 分钟才通知，建议 #4 完整修复先于 P1。
2. **#64 `HISTORY_TOO_LARGE`**：brain thread 是长期会话，最易触发。对策：brain thread 设置轮换阈值（例如 turn 数或估算体积），超限时新建 brain thread 并注入记忆摘要，旧 thread 保留；执行 thread 遇到时转 `attention`，不反复整份重读（0129 熔断）。
3. **写结果不确定**：native 回执不等于受理（AGENTS.md、0069）；派发、追加与 brain 驱动都必须读回 exact `clientMessageId`；补充回答 `ok:true` 不可当成功；异步问题回答尚未开放（#63）。
4. **主机要求**：执行主机必须运行 Codex Desktop 与 Ariel Desktop Agent；大脑主机必须运行 Ariel Desktop Agent（开启 `puckBrain`），并安装 Codex Desktop 应用包、保持 Codex 登录有效，Desktop 本身可不运行。
5. **并发写**：用户与助理同时向同一执行 thread 发送，可能出现 `NATIVE_STATE_UNCERTAIN`；Service 对同一 thread 串行写入，runtime 非 idle 时返回 `TURN_BUSY` 给 Brain（Q9）。
6. **服务端持有更多数据**：开启 Puck 后，对话、记忆、任务摘要存于 Relay 数据卷；需磁盘加密／备份策略与访问控制，并在部署文档中说明（Q17）。
7. **凭据**：VAPID 私钥只存私有文件，不进仓库、日志、通知或测试快照；大脑主机上不新增任何凭据（Q13）。
8. **常驻 brain 进程（Q24 决定带来）**：
    - 实验性 API：`dynamicTools`／`item/tool/call` 是 app-server 实验接口，Codex 升级可能改变或移除。对策：Agent 记录并门禁已验证版本，未验证版本 `puckBrain` 报告降级而非盲用；兼容性探针纳入升级流程；备选方案见 §4.3.7。
    - 工具集不可热更新（openai/codex#24808）：升级工具需轮换 brain thread（§4.2.5）。
    - 与 Desktop 共用 `CODEX_HOME`：探针中未见锁冲突（仅短时低频）；brain thread 在 Desktop 侧边栏可见（Q27 接受），以命名与误输入守卫（§4.2 第 11 条）缓解；用户在 Desktop 中删除或归档 brain thread 时，Agent 下次 resume 失败即按“轮换 brain thread”重建并在对话中说明。
    - 进程资源：常驻进程在大脑主机上长期占用内存；Agent 记录 RSS 并上报，超阈值告警（阈值 P0 实测后定）。
    - 用户可见性：brain thread 不在 Desktop 中，用户无法在 Desktop 里直接续聊或查看 brain 的完整过程，只能在 Puck 界面看对话与“执行 M 步”详情（这是 Q24 的有意取舍）。
    - Brain 的本地读取面：默认只读沙箱下内置 shell 可读主机文件（探针实测）；以进程级 `--disable` 关闭（Q28 已决），并审计事件：brain turn 中出现 `commandExecution`／`fileChange`／子 agent 项即中断该 turn、`puckBrain` 降级并告警。
    - 设置漂移：sandbox 不随 thread 持久，Agent 每次 resume／turn 显式携带策略；Codex 升级后由兼容性探针复核。
9. **B5／B6 非完备**：见 §5.2 说明。
10. **同进程耦合（Q10 决定带来）**：
    - 发布耦合：助理修复要随 Relay 发布，Relay 升级会短暂中断助理。对策：同版本发布与回滚演练；schema 迁移向前兼容一个版本；`--enable-puck` 可在不改镜像的情况下单独关闭 Puck 作为止血手段。
    - 缺陷外溢：助理的 panic、死锁、内存泄漏、CPU 打满或阻塞可能拖垮 Relay。对策：§4.1.2 的 supervisor／recover／熔断；Relay 侧调用一律带超时、永不阻塞等待模块；独立内存预算与队列上限；`go vet`、`-race` 覆盖 puck 包；代码评审禁止在模块中使用 `os.Exit`／`log.Fatal`；`/healthz` 只反映 Relay 核心健康，助理状态另行暴露，避免助理故障触发容器重启；运行期指标（模块内存、队列深度、goroutine 数、熔断次数）。
    - 无法隔离的故障：Go `fatal error`、OOM 杀进程仍会让整个 Relay 重启。对策：内存限制留余量；出现此类事件后可立即关闭 Puck 开关回到现状。
    - 攻击面：不新增公网工具端点；新增面只有 Web 认证的 `puck.*`／`/api/puck/*` 与 Agent 连接上的 `puck.brain.*`（只接受已认证 brain 设备、限速、审计），均仅在开启时注册。

## 13. 开放问题

### 已决

- **Q1 派发确认粒度** — 已决（2026-11-03）：不逐次确认；仅 A 类原生交互与 B 类敏感动作需要授权（§5）。
- **Q2 部署形态** — 已决（2026-11-03）：助理部署在 Relay 侧；Brain 经 Relay／Desktop Agent 驱动；支持多助理（§4、§8）。同日由 Q10 修订：不是独立进程，而是 Relay 进程内的第二入口。
- **Q4 通知渠道** — 已决（2026-11-03）：先做 Web Push（§10），其他渠道以后另批。
- **Q10 代码归属与进程形态** — 已决（2026-11-03）：同一仓库、同一二进制、同一进程；`internal/puck` 模块，配置开启、默认关闭；不做 `cmd/ariel-puck`、不使用 `role=assistant` 连接与服务 API key（§4.1、§7.1）。
- **Q20 内存预算** — 已决（2026-11-03）：接受模块默认 128 MiB 独立预算；开启时 `GOMEMLIMIT` 768 → 896 MiB、容器内存 1 GiB → 1152 MiB（§4.1.1、§4.1.3）。
- **Q21 数据卷** — 已决（2026-11-03）：与 Passkey 共用 `/data` 卷，Puck 数据位于独立子目录 `/data/puck/`（§4.1.3、§8.1）。
- **Q22 PIN 模式** — 已决（2026-11-03）：v1 仅 Passkey + HTTPS 部署提供 Puck；PIN 模式或非 HTTPS 时开关被拒绝，Relay 照常启动，记录原因并在 Web 显示不可用原因；PIN overlay 无需改为 PVC（§4.1.4）。
- **Q7 工具防滥用** — 随 Q13／Q24 解决：无 MCP 端点与主机侧凭据；工具只存在于常驻进程中的 brain thread，调用身份由连接 `deviceId` + brain thread 映射，执行 thread 不声明工具（§4.3、§8.4）。
- **Q3 Brain 运行参数** — 已决（2026-11-03，app-server 路径）：schema 显示 `thread/start` 支持 `developerInstructions`／`approvalPolicy`／`sandbox`／`config`，`turn/start` 支持 `approvalPolicy`／`sandboxPolicy`／`model`／`effort`；人设走 `developerInstructions`，Brain 策略按 thread／turn 设置（§4.2）。Brain 侧余下验证并入 Q27／Q28。
- **Q14 执行主机审批策略** — 已决（2026-11-03）：用户全局为 `never`／`danger-full-access`，故派发一律显式 `on-request` + `approvalsReviewer: user`；无法强制时拒绝自动派发并以 B10 请用户授权；不改用户全局配置（§4.4）。
- **Q24 Brain thread 由谁持有** — 已决（2026-11-03，用户选项一）：大脑主机 Ariel Desktop Agent 自持的常驻 `codex app-server` 子进程长期持有，不交给 Codex Desktop，只在 Puck 界面呈现；执行 thread 仍是 Desktop thread（§4.2）。
- **Q13 工具接入方式** — 已决（2026-11-03，随 Q24）：方案 B，`dynamicTools` 由 Agent 承接并经既有 Agent↔Relay 连接交给模块；不发放 MCP 凭据、不依赖 Desktop 环境变量、不提供 `/api/puck/mcp`；HTTP MCP 仅作为文档化备选（§4.3.7）。
- **Q15 助理工作区与 `puckBrain` 能力** — 已决（2026-11-03，随 Q24）：`puckBrain` 是 Agent 的显式开关（默认关闭，即“允许此主机当大脑”的授权）；负责常驻 app-server 生命周期（启动、监督、退避重启、崩溃熔断、随 Agent 退出）、二进制选择（与 Agent 一致：`ARIEL_CODEX_BINARY` 覆盖，否则 Desktop 内置 codex，版本门禁）、每助理的空 `cwd` 目录（`~/.ariel/puck/<assistantId>/`，0700，不写入 `AGENTS.md` 或 MCP 配置）（§4.2）。

### 仍开放（含受决定影响的更新）

- **Q5 交互事件**：新增轻量 `interaction.pending` 事件，还是从完整 cache 的 `pendingInteractions` 提取？
- **Q6 任务来源**：是否把 Obsidian（0095／0096）或 Chilling（0091）接为某个助理的可选任务来源？
- **Q8 完成语义**：本稿暂定 `review → done` 由 Brain 判定、用户可重新打开；是否改为必须用户确认？
- **Q9 并发写**：执行 thread 忙时，是报 `TURN_BUSY` 由 Brain 稍后重试，还是开放 `queue.add`？
- **Q11 记忆存储形式**（受 Q2 影响）：记忆现位于 Relay 数据卷的 SQLite，Obsidian 直接编辑不再自然；是否只在 Web 设置中编辑，另提供导入／导出 Markdown？
- **Q12 大脑主机离线**（受 Q2 影响，部分缓解）：Puck 模块在服务器侧 Relay 进程内，离线时跟踪与通知继续，仅推理暂停（§8.5）。是否还需要为助理配置备用大脑主机自动切换？
- **Q16 删除与保留期**：助理删除后数据保留多久（暂定 30 天软删除）？是否默认归档 brain thread？
- **Q17 服务器数据保护**：对话与记忆存于与 Passkey 共用的 `/data` 卷（Q21），是否需要应用层加密（密钥来源？）与定期备份？
- **Q18 B 类默认值**：§5.2 的默认清单、阈值（并发 3、每小时 10、连续自动追加 3 次）与 `riskTags` 词表是否合适？
- **Q19 助理共享作用域**：多个助理是否允许配置重叠的设备／目录作用域？重叠时同一目录可能有两个助理并行派发。
- **Q23 执行 thread 设置在 Desktop 接管后的持久性**（P0 当前版本结论）：app-server 路径 on-request + workspace-write 生效，逐 turn `sandboxPolicy` 生效并写回持久设置，裸 resume 回落为全局 `danger-full-access`、审批保留；Desktop 26.930.51102／Codex 0.160.0 的 follower 显式 policy fixture 返回 `IPC protocol incompatible`，无法取得设置读回，故保持 **unverified**。P0 不再等待猜测性兼容，按 §4.4 第 4 条／B10 拒绝自动派发；后续版本以同一隔离探针重新验证。brain thread 不迁移，不受此问题影响。
- **Q25 即时确认时延**：常驻进程省去每轮启动与 `initialize`，brain thread 常驻内存，首条确认更可能 ≤ 2 s；但首 token 时延仍取决于模型与网络，`thread/resume`（进程重启后首轮）会更慢。本稿仍以“≤ 500 ms 处理中指示 + 3 s 模板确认兜底”保证 E1，P0 在 Mac mini 实测（热路径与重启后首轮分别记录）。是否接受模板确认，还是要求一律由 Brain 生成？**2026-11-03 实测**（热进程、trivial turn）：首个 delta 1.5–2.5 s，完成 1.8–2.9 s；含工具调用约 12 s。热路径大多 ≤ 3 s，但不稳定低于 2 s，保留“500 ms 指示 + 3 s 模板兜底”；重启后首轮未测。
- **Q27 brain thread 在 Desktop 中的可见性** — 已决（2026-11-03 22:21，用户）：写探针与目视确认表明 brain thread（包括 `threadSource:"chatgpt_hidden"`）会出现在 Desktop 0.160 侧边栏；两进程共用 `CODEX_HOME` 未见冲突。v1 接受为已知外观行为；仍设 `chatgpt_hidden`（无害，可能对后续版本生效）；命名「[Puck] <名称>」；以信封 nonce + `developerInstructions` 守卫识别并处理 Desktop 中的误输入，递归守卫照常适用（§4.2 第 11 条）。
- **Q28 Brain 的本地能力**（已决）：Brain 取 `approvalPolicy: never` + `sandbox: read-only` 时动态工具是否不受审批影响；能否通过 thread `config` 禁用内置 shell／文件读取，使 Brain 只能经 Puck 工具行动？不能禁用时是否接受“只读 + 硬规则 + 审计”？**已决（2026-11-03 探针）**：可禁用，采用 §4.2 第 3 条 D 组合 + `never` + read-only + 事件审计；`code_mode_host` 必须保留。残余（code mode `exec`、`request_user_input`、`collaboration.*`）在实现时以版本门禁与审计兜底。
- **Q26 默认入口**（新）：开启 Puck 后，Web 打开时默认进入“Puck”视图还是保持现有“会话”视图（本稿暂定记住上次选择、首次进入“Puck”）？

## 14. 不变量（实现期间不得违反）

1. Codex 仍是执行会话与状态的 SSOT；Relay 核心（会话热路径）不持久化、不读取任何 Puck 数据；持久状态只存在于开启后的 `internal/puck` 模块。
2. A 类原生交互只由用户决定；Brain 与工具调用不能产生 `userDecisionId` 或批准授权。
3. 关键动作的执行内容必须与用户批准时冻结的 `actionHash` 一致。
4. 任何写操作 `unknown` 不自动重放，只重新读取权威状态。
5. 未开启 Puck 时，Relay／Agent／Web 的路由、行为、协议形状、goroutine、文件、内存与测试结果与现状完全一致；未启用 `puckBrain` 的 Agent 不写任何助理文件、不启动常驻 app-server、hello 能力与现状一致。
6. puck watcher 不创建 per-client native follower。
7. 助理之间数据隔离；任何助理不能驱动任何 brain thread。
8. Web Push 载荷不含 prompt、代码、路径、命令或审批详情（可选摘要除外，且 ≤ 120 字符）。
9. 不提交 VAPID 私钥、真实会话内容或本机绝对路径；大脑主机上不为 Puck 工具新增任何凭据或配置文件。
10. Relay 核心 goroutine 永不阻塞等待 Puck 模块；模块的 panic 不得终止 Relay 进程（`fatal error`／OOM 除外，见 §12.10）。
11. Relay hot cache 预算与计量不包含、不受助理影响。
12. `userDecisionId` 只由 Relay 核心签发，Puck 模块无签发能力。
13. 非 Passkey + HTTPS 部署中助理不可开启；开关被拒绝时 Relay 照常启动，Ariel 现有行为不变（Q22）。
14. 助理派发的执行 turn 不得在无法确认 `on-request` 审批的情况下自动执行（§4.4、B10）；不修改用户全局 Codex 配置。
15. 对话中显示的任务状态只取自 §9 状态机；`unknown`／`reconciling` 不显示为成功（E13）。
16. brain thread 只由其大脑主机 Agent 的常驻 app-server 驱动；Codex Desktop、Web 与其他助理都不能向它写入；执行 thread 永不声明 `dynamicTools`。
17. 工具调用的 `assistantId` 只由 Relay 依据连接认证的 `deviceId` 与 brain thread 映射得出，不信任 Agent 或 Brain 自报。

## 15. 分阶段计划与验收

每阶段先更新本规格对应小节并经用户确认为 Approved，再按 red → green → refactor 实施；写操作测试只作用于隔离 fixture（mock agent 或专用 Codex fixture thread），不向业务会话发送、审批或回答；真实 Push 测试只发给专用测试订阅。

### P0：进程内入口 + 单个助理端到端 + Web 入口

范围：Relay 进程内编排接口、开关与路由注册、`puck.*` 进程内分发、`/api/puck/*`（仅 Web 认证端点）、Relay↔Agent `puck.brain.*`、来源标注、brain thread 自引用拦截、supervisor／recover／熔断与独立配额；`internal/puck` 最小形态（SQLite `assistants`／`conversation_messages`／`tasks`／`authorizations`／`audit_log`、Puck CRUD、brain thread 驱动、动态工具执行、策略引擎 A 类 + B1–B3／B5／B6）；Agent `puckBrain`（常驻 app-server 监督、专用 `cwd`、工具调用中转、#4 最小解耦）；Web §1.2 的 UI1（三栏布局与手机抽屉）、UI2（气泡、多段回复、处理中指示、过程折叠、时间戳）、UI3.1／3.2／3.4（问题、授权、任务卡片）、UI4（输入框与停止）、UI5（新建与问候）、UI7（主题与深色模式）及不可用原因提示；执行策略强制与 B10（§4.4）；`puck.stop`；开启前置条件与拒绝行为（§4.1.4）；Docker 文档、K8s `passkey-puck` overlay、ECS 脚本的可选开关（`ariel.sh up local` 不提供）。

前置：只读探针已完成（§4.2，2026-11-03）；写探针（1）（2）（4）（6）与（3）（5）的 app-server 部分已于 2026-11-03 完成（§4.2.2），Q27 已目视确认并接受；余下 follower start-turn 策略（Q23）在 P0 第 0 步完成。Approved 前必须在**隔离 `cwd`、专用 fixture thread** 上完成写探针并记入 `docs/testing/`：（1）常驻 app-server 以 `dynamicTools` + `developerInstructions` + read-only 创建 brain fixture，完成一次 `item/tool/call` 往返（mock 结果，不触达真实执行主机）；（2）进程重启后 `thread/resume` 工具仍可用；（3）Desktop 运行时 brain fixture 是否出现在 Desktop 会话列表、两个进程并发是否冲突（Q27）；（4）Brain 的 `approvalPolicy` 与内置 shell 行为（Q28）；（5）执行 fixture 的 on-request 策略经 bootstrap 与 follower start-turn 生效且读回一致（Q23）；（6）热路径与重启后首轮的首条消息时延（Q25）。未确认项标 unverified，并据此修订 §4.2／§4.4。

验收：

1. 前置条件门禁（red 先行）：PIN 模式、`ARIEL_PUBLIC_ORIGIN` 非 HTTPS、数据目录不可写／被锁定时设置开关，Relay 仍启动成功，输出带固定原因码的 WARN 日志，不创建 `/data/puck/`、不注册路由、不启动 goroutine；hello／auth status 声明 `puck.state=unavailable` 与原因；`puck.*` 返回 `FEATURE_DISABLED` 与同一原因码；Passkey + HTTPS 配置下声明 `ready`。
2. 关闭态回归：未开启时 `/api/puck/*` 404、`puck.*` 返回 `FEATURE_DISABLED`、不创建数据文件、goroutine 数与现状相同；Go 全量、Web 与 Chromium 既有测试全部通过。
3. Relay 编排接口单测：方法白名单；`Origin` 只能由模块按来源填写；独立配额与 `OVERLOADED`；不占用 Web 配额；watch 与 Web 共享 follower；事件队列满时只降级模块订阅、Web／Agent 延迟不受影响（基准对比）。
4. 故障隔离：注入模块 handler panic、后台 goroutine panic、handler 阻塞、SQLite 不可写、数据目录缺失——Relay 继续服务 `/ws` 与 Agent，`/healthz` 正常，助理返回 `PUCK_UNAVAILABLE`；重复 panic 触发熔断；第二个进程打开同一数据目录被文件锁拒绝。
5. 协议兼容：`puck.*` 与新错误码加入 schema；旧 Web／旧 Agent 的 hello／device 结构与既有测试不变（0109）。Passkey 模式下 Web 认证后可调用；不存在 `/api/puck/mcp`（404）；Web 客户端调用 `puck.brain.*` 被拒；非 brain 设备或非当前 brain thread 上送的 `puck.brain.toolCall` 被拒并审计；未开启 `puckBrain` 的 Agent 能力与现状一致。
6. 模块单测：CRUD 全流程含 provisioning 失败重试（复用已记录的 `brain_thread_id`，不重复建 thread）；更换大脑主机新建 brain thread 并 retire 旧 thread；删除后旧 thread 的工具调用被拒、routine 停用、执行 thread 不受影响；工具集版本升级触发轮换；跨助理访问一律拒绝。
7. 策略引擎表驱动测试：A 类不可关闭；B1–B3、B5、B6、B10 命中与放行；`actionHash` 篡改拒绝；授权过期／拒绝／单次消费；Brain 侧无法创建或批准授权。
8. 递归保护：对任一 brain thread 的写／读请求在模块与 Relay 编排接口两层被拒；`userDecisionId` 只能经 Relay 核心签发，模块伪造或重放被拒；派发 `cwd` 落在助理工作区根内被拒；Agent 拒绝为 brain thread 打开 Desktop follower，`thread.list` 结果不含 brain thread。
9. 集成（mock-agent ×2 作为大脑主机与执行主机 + 开启 Puck 的单个 Relay 进程；大脑 mock 模拟 `puck.brain.*`）：创建助理 → 用户消息 → brain turn（读回 exact `clientMessageId`）→ 模拟 Brain 发 `puck.brain.toolCall(start_task)` → 结果经 `puck.brain.toolResult` 返回 → 执行主机 `thread.create` + `turn.start` 且无需确认；高风险 `riskTags` 进入授权卡片，批准后执行冻结动作；`unknown` 不重放；工具调用期间断开大脑 Agent 连接，重连后按 `callId` 读回、不重复执行。
10. Agent brain host 单测（假 app-server 脚本）：二进制选择与版本门禁；`thread/start` wire 含 `dynamicTools`／`developerInstructions`／read-only；`item/tool/call` 往返、超时回复 `success:false`、在途上限；子进程退出后退避重启与 10 分钟 5 次熔断、`turn.lost` 上报；Desktop 健康检查失败时仍以 `codexReady=false`、`puckBrainReady=true` 上线（#4 最小解耦）；执行 bootstrap 路径拒绝 `dynamicTools`。
11. 真实浏览器（Chromium 320／390／1280px）：助理创建／切换／设置、对话、授权卡片、跳转执行 thread；模块熔断时显示“Puck 不可用”、PIN 模式下显示“仅支持 Passkey + HTTPS”的禁用入口，且现有会话功能不受影响；浏览器验收使用测试专用 Passkey + HTTPS harness（自签证书或本地 HTTPS 代理，虚拟 authenticator），不使用线上部署；截图基线 + 行为断言。物理手机待用户复验。
12. 隔离 Codex fixture 上一次真实端到端 smoke（专用 fixture 目录，不触碰业务会话）：真实常驻 brain（隔离 `cwd`）调用 `start_task` → 隔离执行 fixture；读回执行 thread 的 `latestThreadSettings` 为 `on-request`；模拟旧 Agent／读不到设置时拒绝自动派发并出现 B10 授权卡片。
13. 体验与界面（Playwright，Chromium 320／390／1280 px，浅色＋深色，固定时钟与 Asia/Shanghai 时区，mock Brain）：§1.2“界面验收要点”中的布局、消息、即时确认（E1 计时断言）、过程折叠（E3）、问题／授权／任务卡片、停止（E12：中断全部进行中任务并出现汇总）、新建流程与问候；任务卡片状态与状态机一致（E13）；文案检查：默认中文、问候与确认消息 ≤ 2 行（E9）。在大脑主机上实测 E1 首条 Brain 确认时延并记录（Q25）。
14. 部署：容器镜像开启／关闭两种配置启动验证；K8s `kustomize build` 校验（`passkey-puck` overlay 含开关、`/data/puck`、896MiB／1152Mi；PIN overlay 渲染结果与现状逐字节一致）；ECS 脚本 `--enable-puck --mode passkey` dry-run 通过、`--mode pin` 组合报错退出；`ariel.sh up local` 设置开关时被拒绝并给出原因。不对线上部署执行任何操作。

### P1：任务跟踪 + Web Push

范围：`task_events`／`pending_decisions`／`push_subscriptions`／`notification_prefs`／`notifications`；§9 完整状态机与里程碑映射（E3、E4）；UI3.3／3.5／3.6（Codex 审批卡片、附件卡片、通知权限卡片）、侧栏待处理角标、信息栏“任务”“通知”标签页；watcher；A 类交互推送与 `puck.decision.record` 门禁；VAPID、`/sw.js`、订阅管理、按助理／类型开关；通知中心与按助理 avatar descriptor 生成／缓存通知 PNG；B7–B9；多助理共享大脑主机的 brain turn 并发上限；brain thread 轮换（#64 对策）。UI2.3 的轻量表情回应与账号级全局未读水位已按 §1.3 提前实现；若未来引入稳定 reader ID，再在 P1 扩展为设备级已读水位，不能以浏览器临时 ID 冒充账号设备身份。

验收：

1. 状态机表驱动测试覆盖 §9 全部迁移与非法迁移，含 `awaiting_authorization` 各出口与连续自动追加上限；Relay 进程重启或模块熔断恢复后从 SQLite 恢复并重新读取权威状态，不重复创建 thread、不重发写请求。
2. watcher：模块与 Web watcher 共享单一 Agent follower（断言 follower 数＝1）；断线重连后按任务表恢复。
3. Web Push 单测：VAPID 生成与持久化；载荷加密（RFC 8291 测试向量）；载荷字段白名单与 ≤ 2 KiB 快照断言（不含 prompt／路径／命令）；TTL／Urgency；404／410 删除订阅；连续失败暂停；去重、合并与每分钟上限；按订阅 × 助理 × 类型过滤。
4. 通知触发：完成、失败、审批、授权、补充问题、设备离线（≥ 2 分钟）各且仅各一条；streaming 零进度通知。
5. 大脑主机离线：该主机上所有助理进入 `brain_offline`，任务跟踪与推送继续，摘要降级为模板；其他助理不受影响。
6. 真实浏览器：Chromium 中授予通知权限、订阅、接收真实 Web Push（本地测试推送服务或真实 FCM 专用测试订阅）、`notificationclick` 深链到卡片；Service Worker 不拦截其他请求（断言现有页面加载与升级流程不变）；iOS 非独立模式显示添加到主屏幕引导。物理 iPhone（16.4+ 主屏幕 PWA）待用户复验。
7. 隔离 Codex fixture 上完成一次“派发 → 原生审批 → Push → 用户在卡片决定 → 完成 → 摘要 → Push”。
8. 体验与界面（Playwright）：mock 事件序列下对话中只出现里程碑消息（开始／阻塞／结果各一条），其余变化只更新卡片（E3）；完成／失败／需要用户时对话消息与 Push 同时产生（E4）；Codex 审批卡片在对话内且与现有卡片行为一致，Push 深链定位到同一卡片（E5）；附件卡片打开现有查看器（E11）；通知权限卡片只出现一次、拒绝后不再出现；侧栏未读／待处理角标与已读水位多标签页同步。
9. 资源：开启 Puck 的长稳基准（例如 50 个助理、50 个跟踪任务、持续事件）下记录模块内存、goroutine、队列深度与 Relay `/ws` 延迟，确认模块不超出 128 MiB 预算、进程在 896 MiB `GOMEMLIMIT`／1152 Mi 限制下稳定、Relay hot cache 行为不变（Q20）。

### P2：记忆 + Routines

范围：`memories`、`routines` 与工具；scheduler（时区、错过只补一次、`preauthorized_scope`）；信息栏“记忆”“例行”“主机与权限”标签页（UI6），对话中“已记住”标签（E7）与例行卡片／一句话询问（E8）。

验收：

1. 记忆按助理隔离；读写、范围、删除后 Brain 不再读到；写入时拒绝高熵 token 样式文本；B9 命中授权。
2. 确定性时钟测试：cron／RRULE、Asia/Shanghai、离线错过只补一次、禁用后不触发、删除助理后不触发。
3. routine 预授权范围内派发免授权、范围外进入授权；事件唤醒中的 routine 变更命中 B7。
4. 真实浏览器（Playwright）：“记住我……”后出现可撤销的“已记住”标签，记忆页可编辑与遗忘，遗忘后 Brain 不再读到（E7）；“每天早上 9 点……”产生例行卡片或一句话询问，例行页显示自然语言计划、下次运行（Asia/Shanghai）、暂停／恢复（E8）；routine 触发后的对话消息与 Push。

### P3：多助理体验完善

范围：助理数量上限（默认 50，已提前实现）、头像、按助理未读／待授权角标、作用域重叠提示（Q19）、删除保留期清理（Q16）、导入／导出（Q11）。验收随对应开放问题定稿后补充。

### P4：CLI-only 主机 Adapter

另起规格：基于 `codex app-server` 让 Desktop Agent 在无 Desktop 的主机上自持 thread owner，可作为执行主机（大脑主机已由 Q24 的常驻 app-server 覆盖）；提取通用 Adapter 接口与能力一致性测试，`codexReady` 等 Codex 专有字段向通用能力迁移且保持 v1 兼容。本规格只登记方向。

## 16. P0 实施计划（已批准，按序执行）

顺序即依赖顺序；每步先写失败测试（red）再实现（green），每步结束时 Go 全量（含 `-race` 覆盖新包）、`web` 的 `npm test` 与既有 Playwright 用例保持通过。写操作只针对 mock agent 或隔离 fixture。

| # | 任务 | 主要文件／包 | 测试 |
| --- | --- | --- | --- |
| 0 | **写探针（2026-11-03 已完成，见 §4.2.2 与 probe results；Q23 follower 路径 unverified，按 B10 fail closed）**：在隔离 `cwd` 中由常驻 app-server 创建 brain fixture thread（`dynamicTools`、`developerInstructions`、read-only），验证：工具调用往返、`thread/resume` 后工具仍在、Desktop 会话列表是否出现该 thread（Q27）、Brain 的 `approvalPolicy` 与内置 shell 行为（Q28）、与运行中 Desktop 共用 `CODEX_HOME` 无冲突、首条消息时延（Q25）；另验证执行 thread 的 on-request 策略经 bootstrap 与 follower start-turn 生效（Q23） | `internal/probe`（新增受 guard 保护的 execution-policy／delete 子命令）、`docs/testing/0130-probe-results.md` | 探针本身即证据；Q23 标 unverified，并按 §4.4 拒绝自动派发 |
| 1 | 协议扩展：`puck.*`（Web）、`puck.brain.*`（Relay↔Agent）、新错误码、hello 中 `puck` 状态与 `puckBrain`／`puckBrainReady`／`executionPolicy` 能力 | `protocol/v1.schema.json`、`protocol/schema.go`、`protocol/contract_test.go` | schema 契约测试；旧 Web／旧 Agent 帧不变（0109） |
| 2 | 开关与前置条件：`--enable-puck`、Passkey + HTTPS 校验、数据目录与文件锁、WARN 原因码；关闭态零影响 | `cmd/relay/main.go`、`internal/puck/config.go` | §15 P0 验收 1、2 |
| 3 | Relay 进程内编排接口：`Devices`／`Forward`／`Watch`／`DeviceStatus`／`BrainThreads`，方法白名单、`Origin` 标注、独立配额、有界事件队列；`puck.brain.*` 只许模块调用；Agent 上送 `puck.brain.*` 事件按连接 `deviceId` 路由给模块；brain thread 拦截（含 Web 请求） | `internal/relay/puck_bridge.go`（新）、`internal/relay/server.go` | 验收 3、8；Web 调 `puck.brain.*` 被拒 |
| 4 | 模块骨架：supervisor／recover／熔断、SQLite（纯 Go 驱动）与迁移、`assistants`／`conversation_messages`／`tasks`／`authorizations`／`audit_log` | `internal/puck/{module,supervisor,store}.go` | 验收 4；迁移向前兼容 |
| 5 | 策略引擎（A 类、B1–B3／B5／B6／B10）与授权（冻结、`actionHash`、单次、过期）；`userDecisionId` 门禁留在 Relay 核心 | `internal/puck/policy.go`、`internal/puck/authz.go`、`internal/relay`（门禁） | 验收 7 表驱动 |
| 6 | 工具 handler（§6.1）：`list_devices`、`list_threads`、`read_thread`、`start_task`、`send_turn`、`interrupt`、`get_task_status`、`request_authorization`、`ask_user`、`share_result`、`react`；幂等 ID、有界结果、作用域 | `internal/puck/tools/*.go` | 单测 + mock 编排接口 |
| 7 | 执行策略强制（§4.4）：bootstrap 的 `thread/start` 与首个 `turn/start` 带 `approvalPolicy`／`approvalsReviewer`／`sandbox`；生产 follower start-turn 带逐 turn 策略；读回 `latestThreadSettings`；`executionPolicy` 能力；不可强制时 B10 | `internal/desktopagent/appserver_bootstrap.go`、`internal/codex/desktopipc/production.go`、`internal/codex/desktopipc/settings.go`、`internal/codex/appserver/execution.go` | 单测断言 wire 形状；验收 12 |
| 8 | Agent brain host：`puckBrain` 开关、常驻 app-server supervisor（二进制选择复用 `probe.SelectBinary`、退避重启、崩溃熔断）、目录创建（仅 `~/.ariel/puck/` 下）、`thread/start`（`dynamicTools`）／`turn/start`／`turn/interrupt`、`item/tool/call` ↔ `puck.brain.toolCall`／`toolResult` 中转与超时、事件裁剪、`thread.list` 过滤 brain thread、拒绝对 brain thread 开 follower；#4 最小解耦（Desktop 未运行仍以 `puckBrainReady` 上线） | `internal/desktopagent/brainhost.go`（新）、`internal/desktopagent/agent.go`、`cmd/desktop-agent/main.go`、`internal/codex/appserver/interactive.go`（如需服务端请求分发辅助） | 用假 app-server（stdio 脚本）测往返、超时、重启、resume、限额；Desktop 不可用时的能力声明 = 验收 10 |
| 9 | 模块业务：Puck CRUD 与 provisioning（`puck.brain.ensure` + 问候首轮）、对话与 E1（处理中、3 s 模板确认）、brain 事件 → 气泡与工作摘要、任务记录与里程碑、`puck.stop`、brain 轮换（工具集版本） | `internal/puck/{assistants,conversation,brain,tasks,stop}.go` | 验收 6、9（mock-agent ×2，扩展 `internal/mockagent` 支持 `puck.brain.*`） |
| 10 | Web：“Puck｜会话”切换、三栏布局与手机抽屉、气泡与多段回复、处理中指示、过程折叠、问题／授权／任务卡片、输入框与停止、新建流程、不可用原因、深色模式与时区 | `web/src/puck/*`（新）、复用 `markdown*.tsx`、`MermaidDiagram`、`interaction-card.tsx`、`style.css` 主题变量、`viewport.ts` | Vitest 组件测试；`web/e2e/puck-*.spec.ts`（320／390／1280、浅／深色、固定时钟与 Asia/Shanghai）= 验收 11、13 |
| 11 | 部署：`passkey-puck` overlay、Docker 文档、ECS `--enable-puck`（仅 passkey）、`ariel.sh up local` 拒绝 | `deploy/kubernetes/overlays/passkey-puck/`、`docs/operations/container-deployment.md`、`scripts/deploy-ecs.sh`、`scripts/ariel.sh` | 验收 14（`kustomize build`、dry-run）；不触碰线上 |
| 12 | 隔离 fixture 端到端 smoke：真实常驻 brain（隔离 `cwd`）→ `start_task` → 隔离执行 fixture（on-request 读回）→ 结果回对话；记录 E1 时延 | `tests/`（新 smoke 脚本）、`docs/testing/` | 验收 12 |

P0 完成定义：§15 P0 验收 1–14 全部通过并有证据文档；未验证项明确标注；用户确认后方可进入 P1。
