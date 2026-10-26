# 0110：按会话选择模型、推理强度与速度

- 状态：Implemented（原能力已随 v0.9.0 部署；2026-10-26 同排展示调整经 TDD／Chromium 验证，待发布）。
- 来源：用户希望像 Codex 一样，在每个会话中选择模型、模型使用的强度和是否加速。
- 关联：0076 首条消息 bootstrap、0100 Relay 双 turn cache、0102 低延迟发送、0109 Web wire 向后兼容。

## 目标与范围

1. 在 composer 中展示当前会话的模型、reasoning effort 和速度档位；支持已有会话和本地新会话草稿。
2. 用户明确选择后，设置真实传给当前执行设备上的 Codex，并从后续新 turn 生效；不是只保存在 Web 的展示偏好。
3. 各设备、各会话独立；切换会话不串用设置。Codex 是已生效设置的 SSOT，Relay 只保存有界只读投影。
4. 首版只覆盖模型、reasoning effort、service tier。不扩展到权限、sandbox、approval policy、工具授权、协作模式切换或自动选择模型。
5. 本规格经批准后，局部替代 0076 中“model 一律继承”的限制：未显式选择时继续继承；显式选择时只覆盖本规格的三个字段，安全设置保持不变。

## 能力证据与未验边界

### 只读验证已完成

1. 2026-10-26 通过当前安装的 bundled App Server 执行 `initialize` 与 `model/list`，成功取得动态模型列表、`supportedReasoningEfforts`、`defaultReasoningEffort`、`serviceTiers` 和 `defaultServiceTier`。
2. 返回的可见模型包含 Fast 档位；其传输标识与展示名不同。生产代码必须使用返回的标识，不能从 UI 文案猜测参数，也不硬编码探测时的模型集合。
3. 当前 bundled App Server 生成的 schema 包含：
   - `thread/settings/update` 的 `model`、`effort`、`serviceTier`；
   - `turn/start` 的同名字段，以及仅对单 turn 生效的 `serviceTierForTurn`；
   - `thread/start` 的 `model` 与 `serviceTier`；
   - collaboration mode 的模型／强度优先级高于独立字段。
4. 对当前 Desktop 安装包的静态检查发现版本化 follower 设置更新接口；处理器可调用 `updateThreadSettingsForNextTurn`，并通过原生状态向 follower 广播结果。
5. Ariel 当前普通发送显式继承 native thread settings，尚未提供上述选择能力；bootstrap 也未传入这些选项。

### 发布准入项（已由隔离 fixture 验证，见文末记录）

1. Desktop follower 设置写入、更新后的原生快照回读、下一 turn 实际采用的参数。
2. 标准速度、加速、恢复继承三者在当前 native 版本中的准确映射，以及 global Fast 打开时能否单独选择标准速度。
3. 运行中修改设置不改变 active turn；有队列时的 native 设置继承行为。
4. Bootstrap 首 turn 与 Desktop 接管后设置的一致性、重新打开会话／重启后的 native 保留行为。
5. 模型列表不是账户 entitlement 或实际推理成功的保证。不得以列表出现、transport receipt 或 `applied: true` 单独宣称推理可用。

初次设计阶段仅执行只读探测。用户确认后，写探针和推理验证均作用于新建隔离 fixture，并由显式 write gate 开启；没有修改用户已有的业务会话。

### 公开参考

- [Codex App Server](https://learn.chatgpt.com/docs/app-server)：动态查询模型与 reasoning effort。
- [Codex models](https://learn.chatgpt.com/docs/models)：模型／速度能力随版本、账户与工作区变化。
- [配置参考](https://learn.chatgpt.com/docs/config-file/config-reference)：区分全局／新会话默认配置与当前会话设置；本功能不得写全局配置。

## 用户行为

### 展示与选择

1. composer 底部增加紧凑的“模型 · 强度 · 速度”入口。手机使用受 viewport 约束的选项面板；复用已有菜单、焦点和提示样式，不遮挡输入、发送与关闭按钮。
2. 选项来自所选设备的动态 catalog；强度、速度随模型过滤。模型切换后保留仍合法的选项；不兼容选项必须在提交前显式展示拟采用的默认值，不能静默降级。
3. 展示当前已确认值；未知时写“继承 Codex／未确认”，不能把 catalog 默认值伪装成该会话实际值。catalog 已不包含的当前模型可只读展示，不能被擅自替换。
4. 选择面板区分“当前已生效”与“准备应用”。提交后显示小范围 saving，不清空 transcript、不触发整页 loading、不丢输入、附件或选区引用。
5. Fast 及其他 native 速度档位按 catalog 能力呈现，同时显示原生描述中的额度／用量提示；不承诺固定倍数或固定价格。
6. 允许在 active turn 进行中修改后续 turn 的设置，界面明确标注“下一轮生效”；不得因此 interrupt、restart 或 steer 当前 turn。
7. 首版已有 queued follow-up 时禁止修改设置，说明“有排队消息，待队列处理后再修改”。已入队内容不增加一套 Ariel 设置状态机；在没有 queued message 时改好设置再入队的路径，必须验证下一轮真实使用已确认设置。
8. `queue.steer` 只追加当前 turn 输入，不改变当前 turn 的模型／强度／速度；“下一轮设置”与当前执行参数不得混淆。
9. 模型切换后若不支持草稿中已有图片，保留图片与文字，明确阻止发送并提示更换模型或移除附件；不得静默丢附件。

### 新会话与切换

1. 本地新会话草稿可以选择设置，意图按 device + draft identity 隔离；只有发送首条消息才创建 native thread。
2. bootstrap 在同一执行进程创建并启动首 turn，并将明确选择的设置用于首 turn。不得恢复已被否定的空会话创建／跨进程接管路径。
3. bootstrap 失败保留草稿与选项；outcome unknown 不自动创建／重发；接管成功后以 native 实际设置替换草稿意图。
4. 已有会话切换时优先展示该会话的已确认投影；后台设置读不阻塞历史正文展示。stale 投影明确只读，不能据此授权设置变更或发送。
5. Codex App 中的设置变化通过现有持久 follower 信号更新当前 Web 的设置投影；不依赖定时全量读取历史。
6. 未提交选择可以取消；离开会话不得把未提交选择用于另一个会话。更新期间切设备／会话，晚到结果只更新原目标，不能污染当前界面。

## Composer 同排展示调整（2026-10-26）

用户明确要求模型入口和附件／发送按钮同排，并把推理强度与速度改为图标。本次仅调整已批准组件的布局与展示，沿用现有统一设置入口／面板；不增加 API、独立设置按钮、写入或网络请求，不改变模型设置语义。

1. 从输入框下方独立一行移到 composer 操作栏：附件在左，模型名与两枚状态图标组成紧凑入口，取消编辑／停止／发送保持右侧；模型入口与附件、发送按钮垂直居中且不换行。
2. 模型名保留文字及展开符号，长名称单行省略；推理强度用分档柱状图标表达，速度用继承、标准、加速／其他原生档位的可区分图标表达。自定义未知 effort／tier 不猜测高低或加速，显示通用／未知图标。
3. 图标仅表达当前已确认值（新草稿为明确暂存意图）；无 confirmed values 时不伪装成当前默认。图标提供精确 accessible name 和完整 tooltip；打开原设置面板后仍展示各选项文字与描述，手机无需依赖 hover 才能理解设置。
4. 入口与附件、取消编辑、停止、发送的点击区域至少 44×44px；视觉图标统一 16–18px。320px、390px 和桌面布局无交叠或横向溢出；运行中同时编辑排队输入时，优先省略模型名，不能隐藏操作或另起一行。
5. 保存中仍禁发，loading／离线／只读／旧 Agent 不支持的行为保持不变；正文、草稿、附件、引用与输入焦点不因 streaming 或 native settings update 被清空。
6. 图片与模型不兼容的提示保留且独立排版，不让提示把操作栏撑宽或挤掉发送按钮；提示不得改变发送门禁。
7. TDD：先运行会失败的图标语义、App 同排 DOM 和真实 Chromium 几何／交互测试，再修改组件；测试 320／390／1280px、超长模型名、running、queue editing、saving、断线、未知原生值和键盘焦点。新增必要截图，不用截图替代行为断言；物理 Safari 待用户复验。

本轮沿用 frontend-design 的既有 design system 优先原则，复用深色 token、SVG 线条与既有设置面板；[同排图标化验收记录](../testing/2026-10-26-inline-composer-model-controls.md)。

## 公共协议与数据

私有方法、native schema 与 provider 特有字段仅存在于版本化 Adapter。拟提供以下 Ariel 方法：

| 方法 | 输入 | 输出／行为 |
| --- | --- | --- |
| `model.catalog.read` | device target、可选显式 refresh | catalog revision、读／写支持状态、models、每模型 efforts／tiers／默认值／输入模态 |
| `thread.settings.read` | thread identity | settings revision、freshness、当前确认的 model／effort／speed、下一轮生效语义 |
| `thread.settings.update` | thread identity、expected revision、client operation identity、显式 patch | applied／conflict／unknown，以及可证明的实际设置；不启动 turn |
| `thread.settings.watch` | thread identity | 原子 baseline + 后续设置事件；复用现有 hot controller |
| `thread.settings.unwatch` | watch identity | 释放当前 Web 的设置 watch 引用，不关闭其他 watcher 使用的 follower |

1. `settings` 将“未提供／保持不变”“明确选择标准”“恢复 native 继承”分开表达；不能把 JSON `null` 一律当作标准速度或保持不变。
2. effort／tier ID 以 catalog 返回值为准，Adapter 做标准速度与继承语义映射。尚未验证的映射不得显示为可写。
3. 使用独立的设置事件与契约，不给既有 `ThreadView`、cache event、device status 或旧 hello payload 无条件增加字段。遵守 0109。
4. 新 Agent 可以向新版 Relay 声明 execution-settings 能力；Relay 在旧 Web 的 device／capability 投影中必须剔除该扩展。
5. 新 Web 通过首个 `model.catalog.read` 显式选择设置扩展 revision；只向已选择扩展且建立 watch 的连接发送新事件。新版 Relay 遇到旧 Agent 缺失能力时本地返回 unsupported，不把未知 method 转发给旧 Agent，避免其 schema 拒绝导致设备断线。
6. 普通发送请求不增加必要的 catalog／settings round trip。没有 pending 设置且 native revision 健康时，继续使用现有继承设置的快速发送；设置保存期间只阻止同会话的新发送，完成后恢复。
7. Bootstrap 允许携带显式设置意图；旧 Web 不带这些字段时行为不变。新 Web 只有在当前 Agent 声明并验证支持时才发送这些字段。
8. 事件携带公开的 device／Agent epoch、thread identity、单调 revision 与 watch identity。旧 epoch、旧 owner、已释放 watch、重复或倒退事件不得覆盖新设置。

## Agent／Adapter 写入和并发约束

1. 已有 Desktop 会话沿用当前 hot controller、owner fence 与版本化 follower mutation；不另起 App Server 接管／执行同一个已有会话。
2. 设置更新与发送按同一 thread 的 mutation 锁排序。写前验证 fresh owner、当前 revision、模型组合及 queued message 状态；失败不调用 native。
3. 只白名单传入 model、effort、service tier；不得接受 Web 提供的任意 native settings map，不改变 collaboration mode 的模式或 developer instructions。
4. 验证 collaboration mode 的优先级与 native 设置合并；若改变模型／强度需要调整其同名字段，由 Adapter 保留其余字段后处理，不能让旧 mode 设置悄悄覆盖用户选择。
5. expected revision 用于 Agent 侧乐观冲突检测，native 支持的条件更新同时使用。不能宣称它是与 Codex App UI 的跨进程原子 CAS：native 并发保留其原有竞争语义，最终 UI 始终显示最新原生回读结果。
6. transport receipt／`applied` 与 native 状态确认分开。只有匹配目标及设置内容的 native snapshot／patch 或有界元信息回读才能把 UI 标成确认；不下载完整历史以确认三个标量字段。
7. 断线／超时后结果不明时返回 unknown，保留原显示并提示核对；仅做只读对账，不自动重放设置变更或消息。重连后重新读取，不把 Web 偏好自动写回 Codex。
8. Bootstrap owner、Desktop owner、leased／limited wrapper 均须正确暴露其实际支持能力；不支持时 fail closed，不出现“底层支持但 wrapper 丢失”的假支持。

## 性能与资源

1. catalog 按 device + Agent epoch + Adapter／账号可见能力上下文共享，成功结果 TTL 默认 5 分钟，in-flight singleflight；切换同设备会话不逐会话调用 `model/list`。
2. 设备断线／epoch 或 capability 变化使 catalog 失效；用户可显式刷新。不得使用旧 catalog 授权当前 mutation。
3. catalog 支持 native 分页，但累计最多 512 models、8 pages、1 MiB 规范化数据；达到限制明确返回 capacity error，不能静默截断后声称列表完整。
4. 单会话设置投影上限 4 KiB，ID／显示名／描述等设置长度与集合数量有契约上限；只在设置或 freshness 真正变化时发布，不随每个 token 重发。
5. 设置 watch 与当前已有 hot controller 共享 follower；不新增 per-Web follower、不遍历全部会话、不做 background polling。
6. 不改变最新两个 turn 缓存、旧页最多三个 turn、图片／tool lazy load 或总体内存预算；不因打开模型菜单加载 transcript。

## 错误与降级

| 条件 | 行为 |
| --- | --- |
| 旧 Agent／当前 native 不支持 | 隐藏可写入口或显示只读原因；保留原发送能力，不影响设备连接 |
| catalog 读取失败／超限 | 局部错误与重试；原会话内容和既有发送链路继续可用 |
| 原生未确认／owner stale | 当前设置只读；不得假显示保存成功 |
| model／effort／tier 组合非法 | 写前拒绝，提示重新选择；不得静默换模型／降速 |
| native 明确拒绝／账户不可用 | 展示可操作的错误，保留已确认设置与输入，不自动换模型重发 |
| revision 冲突／native 并发修改 | 展示最新确认值并请用户重新选择，不盲目覆盖 |
| mutation 超时／断线 | unknown + 有界只读核对；禁止自动重放 |
| native 对 standard／inherit 映射无法验证 | 该速度写选项保持 unverified／禁用，不伪造支持 |

## TDD 与验收

规格确认后，先实际运行失败用例，再写实现；每个阶段分别保留 red／green 证据。

1. Catalog：分页、动态强度／tier、无 tier、隐藏模型、空列表、未知 effort ID、singleflight／TTL、超限与错误；证明多个会话只复用同设备 catalog。
2. Native normalizer：实际 thread settings／collaboration mode 优先级、缺失／null／standard 语义、next-turn 与 active-turn 参数分离、仅设置 patch 时仍产生设置更新。
3. Mutation：精确 thread 与 owner、revision 冲突、并发发送顺序、queued message 禁改、非法组合不调用 native、拒绝／unknown 不标成功不重放、wrapper 能力不丢失。
4. Bootstrap：显式三项用于首 turn、默认继承、安全参数不变、图片模态校验、接管后设置一致、失败保留草稿与选择。
5. Wire：用上一版真实严格 schema 验证旧 Web 全部既有消息；旧 Agent 不接收未知 method；设置事件仅发给 opt-in watcher；旧事件、旧 epoch、退订后事件被 fencing。
6. Web unit：会话／设备／草稿设置隔离；切换模型时合法选项保留与不合法值显式确认；保存／冲突／unknown；App 侧设置更新；发送期间不丢草稿与附件。
7. 真实 Chromium 桌面与手机 viewport／touch：选择、取消、保存、键盘、长名称、面板滚动、焦点、streaming 时打开菜单、切会话晚回包、后台恢复；断言几何、行为并保存必要截图。不得将模拟验收称为物理 Safari 验收。
8. 真实 Codex 隔离 fixture compatibility gate：
   - model／effort／Fast／标准／继承均经写入、native 回读与下一 turn 实际参数验证；
   - active turn 不变、下一轮生效，设置后入队行为不丢选项；
   - App UI 修改能反馈到 Web，两个会话互不影响；
   - native 重新打开／Agent 重连／bootstrap 接管行为有明确证据；
   - 只记录支持结果、时延与脱敏断言，不提交真实 thread ID、正文、账号配置或原始 IPC 快照。
9. 回归 Go／race／vet、Web 全量、真实浏览器 suite 与 build；证明普通 send 不增加 RPC 往返、不重同步历史、不破坏最终 turn 完成更新。

## 发布与回滚边界

1. 只有通过对应真实写验证的能力才宣告 supported；有 partial 支持时分别报告 model／effort／speed，不把整体包装为全支持。
2. 发布顺序 Relay／Web → Desktop Agent；首次能力声明前旧 Agent 可正常读写会话。旧 Web 长期开着也不收到新字段或新事件。
3. 回滚使用匹配的 Relay + Web 镜像，并关闭新扩展；不通过回滚把 Web 暂存值写回 native，也不回退已成功执行的消息。
4. 用户已批准本规格的实现与隔离 native 写探针；生产部署另按交付请求执行。

## 实现补充与版本开关

1. 已有会话通过 follower 设置写入并等待原生 metadata patch 确认；不调用完整历史 refresh。Reducer 在已有解码过程中准备最多 4 KiB 的 model／effort／tier 投影，设置 read／watch 不复制或再次解析历史。
2. 设置 revision 使用 owner generation 与递增版本，避免观察到 A → B → A 后复用旧 revision。generation 只保留随机标识，不额外持有已关闭的 native follower。
3. 当前设置写能力的真实兼容性组合为 Desktop `26.930.51102` + Codex `0.160.0`。CLI 对未通过本项验证的组合关闭新扩展，不据此关闭或放宽既有会话和 task 的其他能力。
4. 可使用 Desktop Agent 的 `--disable-execution-settings` 或 `ARIEL_DISABLE_EXECUTION_SETTINGS=true` 关闭新扩展；关闭时 Agent hello 不携带新增 capability，可配合旧 Relay 回滚。
5. Web 与 Relay 的新方法／事件使用独立设置契约。旧 Web device projection 不包含新增 capability；旧 Agent 不会收到不认识的设置方法。
6. 兼容性与 TDD／浏览器记录见 [0110 验收记录](../testing/2026-10-26-0110-execution-settings.md)。物理 iPhone／Safari 与其他 native 版本仍需各自验证。
7. 真实测试发现 Desktop 首次接管 bootstrap thread 时会用全局 Fast 覆盖首轮的标准速度。显式选择的新会话将初始化意图保留至这一次接管，校验 model／effort 未被并发修改后，在开放后续 mutation 前初始化并确认 native 设置；随后立即丢弃意图。普通恢复／重连不执行此写入，失败保持只读且不重发首条消息。
