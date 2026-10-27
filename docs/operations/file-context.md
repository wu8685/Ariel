# 内置文件能力配置

[文档导航](../README.md) · [功能使用](../guide/features.md#文件与文档) · [Agent 安装](agent-install.md)

文件读取、Diff、消息引用、workspace 目录和 Markdown 图片由 Ariel Desktop Agent 内置提供，不需要安装或启动 Chilling，也不要求 MySQL。Relay／Web 只负责入口和受限转发，不能直接读取桌面文件。

## 开关与前提

| 开关 | 所在端 | 默认 | 用途 |
| --- | --- | --- | --- |
| `--enable-file-context`／`ARIEL_ENABLE_FILE_CONTEXT=true` | Agent 与 Relay | 关闭 | 文件卡片、原文／Diff、消息引用与片段校验 |
| `--enable-workspace-browser`／`ARIEL_ENABLE_WORKSPACE_BROWSER=true` | Agent | 关闭 | 浏览会话当前 workspace 的目录与文件；依赖文件开关 |
| `--enable-document-assets`／`ARIEL_ENABLE_DOCUMENT_ASSETS=true` | Agent | 关闭 | 读取 Markdown 本地图片；依赖文件开关与双端资源协商 |

新旧端只使用实际声明支持的能力；旧端不支持图片时，文本阅读和会话仍可用。启用文件功能不会自动开启目录权限、图片或看板。

## 首次安装

局域网模式的完整示例：

```sh
./scripts/ariel.sh up local \
  --listen '<HOST-LAN-IP>:8080' \
  --device-id 'my-desktop' \
  --device-name '我的桌面设备' \
  --enable-file-context \
  --enable-workspace-browser \
  --enable-document-assets
```

远程 Relay 模式在完整 `up agent` 命令中增加相同三个开关；Relay 还需单独启用文件入口。使用 ECS 部署器时，在首次完整部署命令中追加 `--enable-file-context`；手工 Docker 部署可通过 `-e ARIEL_ENABLE_FILE_CONTEXT=true` 配置。公网仍须使用 Passkey 与 WSS，详见[ECS 部署](ecs-single-node-deployment.md)。

## 更新已有安装

脚本会把开关保存在私有配置中，后续 `up` 复用配置，不靠继承当前 shell 的环境变量。

- 脚本托管：先用 `status` 核对模式与身份，再停止它管理的进程，以完整命令加 `--replace-config` 更新，保留原 token／PIN、设备 ID、Relay 地址和其他配置。不要只传三个开关就覆盖旧配置。
- launchd 或其他托管器：修改实际受管服务中的同名参数／环境变量，保留旧配置和二进制用于回滚；不要用 `ariel.sh stop` 或按端口杀进程来接管它。
- Relay：保留 Passkey key、凭据文件、Agent token、原有容器网络和内存限制，不为开启文件入口重新生成登录凭据。

旧 `--enable-chilling-file-context`／`ARIEL_ENABLE_CHILLING_FILE_CONTEXT` 仅为兼容别名，仍启用内置实现；`--chilling-endpoint` 已弃用并被忽略。新旧 CLI 文件参数显式冲突时拒绝启动。迁移必须核对旧目录授权，不能因以前安装了 Chilling 就推断有权浏览工作区。

## 内容、缓存与限制

- 原文：最多 1 MiB UTF-8 文本；显示的是当前 workspace 版本。Diff 单项最多 64 KiB，截断会明确标识。
- 片段：最多 5 个选区，单个最多 8 KiB、合计 24 KiB；发送前复验 thread、来源、路径、文件 SHA／mtime 和原文，变化时返回 `FILE_CHANGED`。
- 路径：限定在当前会话的权威 workspace，拒绝越界、symlink、`.git` 和特殊文件；不是任意 filesystem 代理。
- 浏览器文件缓存：最多 10 个文件、合计 8 MiB；目录按页获取，不递归预热。Relay 不把文件正文、目录或图片存入双 turn hot cache。
- 本地图片：PNG／JPEG／GIF／WebP，单图最多 4 MiB、12 MP；分块传输，单块原始字节最多 256 KiB。原始 HTML／SVG 不执行。
- 外部图片：浏览器只在明确允许后加载合法 HTTPS 地址，Agent／Relay 不代为代理；允许外图意味着浏览器将连接相应站点。

## 核验与排障

1. 核对 Agent 与 Relay 文件开关。Web HTML 的 `ariel-file-context` meta 为 `true` 只证明入口已开，不能替代目标设备 capability 核验。
2. 在隔离 workspace 中检查目录、原文和阅读模式；带真实 `fileChange` 的 fixture 才能验证文件卡片／Diff，真实 agentMessage 引用才可验证消息入口，不伪造来源。
3. 选中部分文本，手动点击“引用到会话”，检查草稿保留；生产 smoke 不发送、编辑、停止或审批真实会话。
4. `PATH_NOT_ALLOWED` 先查路径、来源与 workspace 边界，不直接放宽根目录；`FILE_CHANGED` 重新读文件和选区；旧端没有资源能力只降级图片，不要求重新登录。
5. 验证“不依赖 Chilling”使用独立 fixture，不停止现有扩展进程或删除其数据。当前原生兼容与未验项见[0119 验收](../testing/2026-10-27-0117-0119-self-contained-files.md)。
