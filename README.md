# Ariel

Ariel 让你离开电脑后，仍能从手机浏览器接着使用 Mac 上的 Codex Desktop 会话。点开已有会话就自动加载原会话；历史、实时回复和运行状态在手机上继续显示。你可以发送消息、停止当前任务、处理命令或文件审批，并回答 Codex 的补充问题。

Ariel 不建立另一份聊天记录。原会话、工作目录和执行权限仍属于本机 Codex。

界面默认采用 Night 配色：深色背景、浅色文字和蓝色操作高亮。

自动加载后以 **Desktop 当前权限**运行，不保证沿用会话最初创建时的 sandbox；页面会显示当前权限。若出现 Full Access 警示，仍可发送，请先确认你愿意让该会话按当前 Desktop 权限继续。

## 当前可用范围

已在本机 Desktop `26.930.31730` / 内置 Codex `0.160.0` 的隔离会话上验证：自动加载旧会话、原 owner 实时同步、发送、精确停止、命令单次允许与拒绝并停止、文件变更单次允许与拒绝、多问题的选项与自由文本回答。Relay／Agent 断开后，页面会重新订阅原会话；不缓存离线指令，也不自动重发结果不确定的操作。

权限请求的展示与原请求范围内的单轮响应已有协议和单元测试，但当前 Codex 的 `request_permissions_tool` 默认关闭，尚未取得真实 Desktop 待审批样例。这一项在真实环境中未完成验收；见[测试记录](docs/testing/2026-10-04-m1-m4.md)。其他未验收边界也列在该记录中。

若 Codex 返回无法确认的原生会话状态，Ariel 会暂停该会话的远程操作并给出提示，不会猜测发送成功或反复重试。隔离并发实验已观察到这种状态，详见测试记录。

首版只面向同一可信局域网、单用户和已存在的本机会话。没有新建会话、文件上传、后台通知或公网部署支持。

## 在可信局域网运行

需要 Go 1.26+、Node.js/npm，以及已启动且版本匹配的 Codex Desktop。以下命令在项目根目录运行；把 `<Mac-LAN-IP>` 换成 Mac 在局域网中的地址，`<long-random-token>` 换成仅自己知道的高强度随机 Agent 口令，`<six-digit-pin>` 换成你设置的固定 6 位数字连接码（可以以 0 开头）。两者必须不同。

先构建 Web 和稳定路径的本地可执行文件（便于 macOS 防火墙按程序放行）：

```sh
cd web
npm ci
npm run build
cd ..
mkdir -p .local/bin
go build -o .local/bin/ariel-relay ./cmd/relay
go build -o .local/bin/ariel-desktop-agent ./cmd/desktop-agent
```

终端 1 启动 Relay：

```sh
ARIEL_TOKEN='<long-random-token>' \
ARIEL_WEB_PIN='<six-digit-pin>' \
ARIEL_ORIGINS='http://<Mac-LAN-IP>:8080' \
ARIEL_LISTEN='<Mac-LAN-IP>:8080' \
./.local/bin/ariel-relay
```

终端 2 启动 Desktop Agent：

```sh
ARIEL_TOKEN='<long-random-token>' \
ARIEL_RELAY_URL='ws://127.0.0.1:8080/ws' \
ARIEL_DEVICE_ID='my-mac' \
ARIEL_DEVICE_NAME='我的 Mac' \
./.local/bin/ariel-desktop-agent
```

手机连接同一局域网，打开 `http://<Mac-LAN-IP>:8080`，输入 6 位连接码。连接码长期有效，但不会存入浏览器；Relay 会签发随机 session 凭据，保存在当前标签页的 `sessionStorage` 中，因此刷新可自动连接。新标签页、主动断开、Relay 重启或 session 超过 24 小时后需重新输入。累计 10 次连接码错误会锁定新的 Web 连接，需重启 Relay 才能解锁；已连接的会话不受影响。Relay 的 HTTP/WS 通信未加密，6 位数字也不能抵御同网段窃听；不要在不可信网络或公网直接暴露端口。

运行前可用 `openssl rand -hex 32` 生成一次随机口令。确认 Mac 的局域网 IP 后，优先把 `ARIEL_LISTEN` 设为该 IP 的 `:8080`，只监听当前可信网卡；若用 `0.0.0.0`，也应确认没有接入不可信网络。macOS 防火墙开启时，应仅允许 Relay 可执行文件的入站连接，不要为了测试关闭整个防火墙。`lsof -nP -iTCP:8080 -sTCP:LISTEN` 可检查监听地址；手机与 Mac 需在可互访的局域网，访客 Wi-Fi 或 AP 隔离可能阻断访问。

若页面可打开但显示“Relay 未连接”，检查网络和 `ARIEL_ORIGINS` 是否精确等于手机地址栏的 Origin（协议、IP、端口均一致）。若提示“连接码未通过”，检查 `ARIEL_WEB_PIN`，错误过多则重启 Relay。若 Relay 已连接但 Agent 离线，检查第二个终端与 `ARIEL_RELAY_URL`、`ARIEL_TOKEN`；若 Codex 未就绪，先检查 Desktop 正在运行以及 [兼容性版本](docs/compatibility/2026-10-03-m0.md)。两个进程都可在各自终端按 `Ctrl+C` 停止；Ariel 不安装后台常驻服务。

第二台 Mac 也可运行 Agent，但本阶段未做跨设备调度；每台设备要使用不同的 `ARIEL_DEVICE_ID`。当前版本对 Desktop 与 Codex CLI 采用明确的兼容性放行，升级后应先跑兼容性探针。

## 开发验证

```sh
go test -race ./...
go vet ./...
cd web && npm test && npm run build
```

只读兼容性探针：`go run ./cmd/probe detect`、`go run ./cmd/probe history`。会产生真实 turn 的隔离 fixture 操作另见[探针说明](docs/compatibility/probe-usage.md)，不要对业务会话执行写测试。

规格见[索引](docs/specs/README.md)，当前限制与实测证据见[测试记录](docs/testing/2026-10-04-m1-m4.md)，待办见[项目 TODO](docs/TODO.md)。后续文档只在 Ariel 仓库维护。
