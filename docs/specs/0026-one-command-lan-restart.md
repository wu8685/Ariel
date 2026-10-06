# 0026：一键发现局域网地址并重启本地 Ariel

- 状态：Implemented（用户于 2026-10-06 明确批准；自动测试与真实 LAN 重启已验证）。
- 背景：Mac 切换 Wi-Fi、手机热点或其他局域网后，已保存的 Relay 监听地址会失效。当前需要手动查询默认网卡 IP，再用完整 `up local --listen ... --replace-config` 命令更新并重启；流程较长，也容易不必要地更换连接凭据。

## 用户可见行为

1. 新增一条无参数命令：`./scripts/ariel.sh restart-local`。它读取已保存的 local 配置，自动发现当前默认路由网卡及其 IPv4 地址，沿用已保存端口、设备 ID 和设备名称。
2. 命令保留现有 Agent token 和 6 位 Web PIN，只更新 Relay 的监听地址、Web Origin 与本机 Agent 的 Relay URL。切换局域网不会无故要求用户更换连接码。
3. 命令完成后输出新的手机访问 URL，并运行等价于 `status` 的就绪检查；Web PIN 仍只通过显式 `./scripts/ariel.sh show-pin` 显示。
4. 若服务未运行，该命令直接启动；若服务正在运行，则只停止并替换 Ariel 自己记录、且进程身份核验通过的 Relay／Agent。不得停止占用目标端口的其他进程。
5. 浏览器 Origin 随 IP 改变，旧页面和旧 `sessionStorage` session 不复用；用户在新地址首次打开时重新输入原有 PIN。

## 地址发现与安全边界

- macOS 上从默认路由取得网卡，再通过系统网络接口读取该网卡的 IPv4 地址；不得从公网网站、DNS、代理环境变量或用户聊天内容推断地址。
- 只接受明确的 RFC1918 私有 IPv4（`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`）。无默认路由、无 IPv4、loopback、link-local、组播、公网或 wildcard 地址均拒绝，并保持现有服务与配置不变。
- 目标监听地址必须精确为检测到的 IP 加已保存端口，不自动改成 `0.0.0.0`。继续沿用可信局域网限定与明文 HTTP／WS 风险说明。
- token、PIN、Desktop socket 和 Codex 凭据不得进入命令行参数、普通日志或配置 JSON；凭据文件内容和权限在重启前后保持不变。

## 执行顺序与失败语义

1. 读取并验证现有配置，要求 `mode=local`；未初始化或当前是 remote Agent 模式时，拒绝并给出首次配置命令，不猜测设备身份。
2. 完成地址发现、Desktop 兼容检查、Go／Node 工具检查、目标端口归属检查和新二进制构建后，才停止现有受管进程，以缩短中断并避免预检失败造成离线。
3. 健康的旧 Relay 若仍占用同一端口但绑定旧 IP，视为本命令可替换的受管进程；新地址端口被非 Ariel 进程占用时，在停止旧服务前失败。
4. 配置使用原子写入，只改变网络相关字段；随后启动 Relay 和 Agent，并沿用 PID 身份、HTTP health、ready file 与 Relay handshake 检查。
5. 启动失败时不轮换凭据、不接管外部进程、不自动重试 Codex 写操作；保留日志并明确报告当前配置和服务状态。已经失效的旧 IP 不自动恢复为“成功”。

## 非目标

- 不自动监控网络变化，不安装 launchd 服务，也不在后台周期性重启。
- 不支持公网地址、自动 TLS、端口映射、UPnP、Tailscale 或跨网络中继。
- 不改变 Codex Desktop 最低版本门禁、Relay 协议和会话数据边界。

## TDD 与验收

1. Red：地址发现测试覆盖 `10/8`、`172.16/12`、`192.168/16`，以及无默认路由、公网、loopback、link-local、IPv6-only、畸形输出和命令失败。
2. Red：restart-local 测试覆盖未初始化、remote 模式、服务未运行、完整运行、部分状态、外部端口占用和受管 PID 身份不匹配。
3. Red：记录 token、PIN、设备身份和端口的重启前后值，确认只有 Listen／RelayURL 改变；预检失败时进程与配置保持不变。
4. Green：在现有 startup 包中实现可注入测试的地址发现与重启编排，Shell 入口只转发参数，不复制业务逻辑。
5. 回归：Go race tests、`go vet`、Web tests／build 全部通过；在当前真实局域网运行一次 `restart-local`，确认输出新 URL、`status` 就绪、HTTP 200、WebSocket 设备在线且只读 `thread.list` 成功。

## 验收标准

- 切换局域网后只运行 `./scripts/ariel.sh restart-local`，即可在新 IP 恢复手机访问。
- 重启前后的 Agent token、Web PIN、设备 ID、设备名称和端口完全一致。
- 新地址精确绑定当前默认私有 IPv4；旧地址不再监听，外部端口和非受管进程不受影响。
- Relay、Agent、Codex readiness 和只读会话列表均通过；任何失败都显式报告且不伪造成功。

## 实施记录（2026-10-06）

- `restart-local` 已加入现有 `scripts/ariel.sh`／startup command，不新增第二套 Shell 编排。地址发现只调用 macOS `route` 与 `ipconfig`，并限制为默认网卡的 RFC1918 IPv4。
- 重启先检查目标端口、Desktop、Go／Node 并完成构建，再停止已核验身份的受管进程；网络配置只原子更新 `Listen`／`RelayURL`，不重写凭据文件。
- 真实运行确认 Relay／Agent PID 均更换，Agent token 与 Web PIN 字节保持不变；随后 `status`、HTTP health、WebSocket hello、设备 readiness 和只读 `thread.list` 全部通过。
- Go race tests、`go vet`、Web 97 项测试与 production build 通过。详见[专项测试记录](../testing/2026-10-06-0026-one-command-lan-restart.md)。
