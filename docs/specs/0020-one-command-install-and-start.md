# 0020：一键安装、配置与启动

- 状态：Implemented（用户于 2026-10-05 确认；自动、隔离端口和现有 LAN 服务已验，物理手机切换后待复验）。
- 背景：当前运行步骤分散在 README 的多条构建命令和两个终端中。需要让人或自动化 Agent 用一个入口完成本机安装、配置、启动，并明确区分“只接入已有 Relay”和“本机 Relay + Agent”。
- 范围：macOS 上的 Ariel 启动与本地运行管理，以及项目内的 Agent 安装文档。不更改会话协议、Codex Desktop 配置、Web 连接码与 Agent 长口令的角色分离，也不提供公网托管或开机自启。

## 命令契约

单一入口为仓库根目录的 `scripts/ariel.sh`，从任意当前目录调用均以脚本所在仓库为根目录。脚本兼容 macOS 自带 Bash，不要求 `sudo`、`jq`、Homebrew 或 `curl | sh`。

| 命令 | 输入 | 结果 |
| --- | --- | --- |
| `up local --listen <LAN-IP:port> --device-id <id> --device-name <name> [--token-file <path>] [--pin-file <path>]` | 首次配置同机模式；明确指定可信网卡 IP 和端口 | 构建 Web、Relay、Agent；保存配置；按顺序启动 Relay 和 Agent；输出手机 URL 与不含凭据的状态 |
| `up agent --relay-url <wss://host/ws> --device-id <id> --device-name <name> --token-file <path> [--allow-insecure-ws]` | 首次配置外部 Relay 模式；Relay 已存在且操作者提供其 Agent 长口令 | 只构建并启动 Desktop Agent；不启动 Relay、不安装 Web/npm 依赖；输出连接目标与状态 |
| `up` | 已保存配置 | 使用原配置与原凭据重建所需产物并启动；已运行时幂等，不创建重复进程或轮换口令 |
| `status`、`stop`、`show-pin`、`help` | 已保存配置；`show-pin` 仅同机模式可用 | 查看受管进程和可达性；只停止本脚本启动的进程；按用户显式命令显示本机 Web PIN；帮助列出准确参数 |

首次 `up local` 未提供凭据文件时，用系统安全随机源生成独立的高强度 Agent 口令和六位数字 Web PIN；首次 `up agent` 必须由操作者以本地文件提供已在 Relay 配置的 Agent 口令，不能自行生成不匹配的口令。`--token-file`、`--pin-file` 是导入源；脚本复制到被 Git 忽略的 `.local/runtime/` 私有文件后使用。再次 `up` 沿用，不自动更换。PIN 不作为 CLI 参数、默认日志或 `status` 输出；若要读取，显式运行 `show-pin`。设备 ID 需稳定，避免重启后浏览器丢失既有设备选择。

已有配置时，`up` 不带参数只做重建与启动；若再次传入与原配置不一致的模式、地址或设备身份，必须报错。只有先 `stop`、再在完整的 `up local`／`up agent` 参数后显式加 `--replace-config` 才能改配置；同模式且未提供新凭据文件时沿用原有凭据，切换到外部 Relay 时则必须另给该 Relay 的口令文件；从外部模式切到本机模式时若未导入 token，生成新的本机长口令，不能复用外部 Relay 的凭据。不静默轮换口令或切换目标。

`up agent` 默认只接受 `wss://.../ws`。确为可信局域网内的 `ws://.../ws` 时需显式 `--allow-insecure-ws`；文档必须说明明文 WebSocket 不适合公网。`up local` 使用现有 HTTP/WS 局域网方案，`--listen` 必须是明确的本机地址，不默认绑定 `0.0.0.0`；脚本据此设置精确的 `ARIEL_ORIGINS` 与 Agent Relay URL。外部 Relay 的部署、TLS、Origin 白名单和 Agent 口令发放由 Relay 管理者负责，本脚本不修改外部服务器。

## 安装、配置与运行不变量

1. 本机模式需要 Go 1.26+、Node.js/npm 和已安装的兼容 Codex Desktop；外部模式只需要 Go 与兼容 Codex Desktop。按项目锁文件执行 `npm ci`，构建稳定路径的二进制。脚本不自动 `git pull`、升级 Codex、安装系统软件、修改防火墙或申请管理员权限。
2. `.local/runtime/` 权限为 `0700`，凭据与配置文件为 `0600`；`.local/` 已被 Git 忽略。配置读取不得 `source`／`eval` 不可信文件。口令不能进入参数列表、仓库追踪文件或日志；现有 Go 进程通过环境变量取口令，同一系统用户仍可能读取自己的进程环境，这一限制须写入文档。
3. 启动前检查输入格式、端口是否已被其他进程占用、依赖是否存在、Desktop 版本是否在已验证范围；错误要给出具体修复建议，不能报告假成功。脚本对本机地址显式绕过继承的 HTTP 代理，避免 Agent 进程存活却无法连到局域网 Relay；外部 `wss` 保留操作者的代理设置。
4. `up` 后台启动受管进程，并把 PID 与不含正文／凭据的运行日志放在 `.local/runtime/`。本机模式先确认 Relay `/healthz`，再启动 Agent；只有 Agent 注册到目标 Relay 且 Codex 就绪，才报告“可使用”。外部模式没有 Web PIN，不伪称能代表 Relay 用户确认设备列表；须区分进程启动、已握手和“尚未核实”。
5. `status` 与 `stop` 只信任当前配置记录且可核对可执行文件路径的受管 PID；PID 丢失、复用或端口被外部进程占用时不杀其他进程。启动失败要清理本次新启动的进程，保留配置和原有进程；重复 `up`、`stop` 均幂等。
6. 当前手工运行的 `8080` Relay／Agent 不在脚本的受管 PID 范围，测试不得停止或替换。上线切换需先复用现有 PIN、Agent 长口令与设备 ID，明确告知短暂重连，再停止旧进程并由新脚本接管；无法安全复用时保留旧实例并报告，不暗中轮换。

## Agent 安装文档

新增 `docs/operations/agent-install.md`，README 提供入口。文档面向执行安装的 Agent，分别给出两种模式的前提、需要用户提供的最小参数、单命令范例、成功信号、只读诊断、常见错误与恢复；明确不索取 Codex 登录凭据、不把 token/PIN 贴入聊天或提交、不在真实会话发测试消息。外部模式要解释 Relay 管理者如何提供 URL 与共享 Agent 长口令；本机模式要说明手机访问地址、`show-pin`、可信局域网限制以及服务停止方式。文档不得包含本机真实密钥、实际会话 ID 或私人路径。

## TDD 与验收

先提交并运行会失败的脚本测试，再实现入口；覆盖双模式的构建范围与环境变量、配置权限和复用、无凭据泄漏、URL/PIN 校验、代理隔离、端口冲突、幂等、受管 PID 安全停止、启动失败回滚、缺依赖及兼容性失败。测试使用临时目录／伪进程或隔离端口，不触碰当前 `8080` 与业务会话。

实现后运行脚本测试、`go test -race ./...`、`go vet ./...`、Web 全量测试和构建。在隔离端口验证本机一键启动与停止；外部模式用隔离 Relay 核实 Agent 注册。真实 `8080` 切换时需保持原连接码和稳定设备 ID，最后由手机人工确认连接与历史；自动测试与同机浏览器不能替代这一步。记录测试证据与未验证边界在 `docs/testing/`。
