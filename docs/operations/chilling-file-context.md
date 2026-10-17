# Chilling 文件上下文能力启用说明

该能力默认关闭。只有 Relay 的 Web runtime flag 与目标 Desktop Agent 的 Chilling capability 同时为 `true`，Web 才展示 turn 文件卡片、文件阅读器和引用入口。

## 1. 启动本地 Chilling backend

在 Chilling 仓库构建并启动只读 Unix socket 服务：

```sh
go build -o ./bin/chilling ./cmd/chilling
./bin/chilling serve \
  --enable-ariel-file-context \
  --socket "$TMPDIR/ariel/chilling.sock" \
  --codex-binary /Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex
```

socket 父目录必须仅对当前用户可访问；服务会把 socket 权限收紧为 `0600`。Chilling 只调用 Codex App Server 的 public read methods，不拥有 Ariel 的 WebSocket，也不发送 turn。

## 2. 启用 Desktop Agent capability

在现有 Desktop Agent 启动环境中增加：

```sh
ARIEL_ENABLE_CHILLING_FILE_CONTEXT=true
ARIEL_CHILLING_ENDPOINT="unix://$TMPDIR/ariel/chilling.sock"
```

也可以使用等价参数：

```text
--enable-chilling-file-context
--chilling-endpoint unix:///absolute/path/to/chilling.sock
```

Agent 启动时必须完成 `file-context/v1` handshake 才会声明 capability。Chilling 不可用、版本不兼容或 socket 不安全时，普通 Ariel 会话能力保持可用，文件上下文能力单独关闭。

## 3. 启用 Relay Web runtime flag

在现有 Relay 启动环境中增加：

```sh
ARIEL_ENABLE_CHILLING_FILE_CONTEXT=true
```

也可以使用 `--enable-chilling-file-context`。重启 Relay 后，检查 Web HTML 中的 `ariel-chilling-file-context` meta 值为 `true`；不要只依据构建时环境判断。

## 4. 验收

1. 打开一个已有已完成 turn 且包含 Codex `fileChange` item 的会话。
2. turn 下方应出现“修改的文件”卡片；初始最多展示 5 项。
3. 点击未删除文件后，桌面端显示侧栏，手机端显示全屏阅读器。
4. 点击行号或选中文本并加入引用；composer 出现引用 chip。
5. 发送时仍只有 Desktop Agent 一个 mutation owner。若文件已变化，应返回 `FILE_CHANGED`，而不是发送旧片段。
6. 关闭任一 flag 或停止 Chilling 后，Web 不应展示半成品入口，普通会话读写不受影响。

缓存仅存在于当前浏览器页面内存：最多 10 个文件、合计 8 MiB；设备 epoch 改变或显式退出时清空。Relay 不缓存文件正文。
