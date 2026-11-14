# Puck 记忆嵌入

记忆原文在 Puck SQLite 里。向量也在同一文件。嵌入进程不常驻。

Mac mini 上把这两样放到 Puck 数据目录（默认由 `ARIEL_PUCK_DATA_DIR` 决定）：

- `$ARIEL_PUCK_DATA_DIR/bin/llama-server`（darwin/arm64 的 llama.cpp `llama-server`）。也可以用 `ARIEL_LLAMA_SERVER` 指向别的路径。
- `$ARIEL_PUCK_DATA_DIR/models/Qwen3-Embedding-0.6B-Q8_0.gguf`。也可以用 `ARIEL_PUCK_EMBEDDER_MODEL` 指向别的路径；哈希仍必须相符。

模型文件来自 Hugging Face `Qwen/Qwen3-Embedding-0.6B-GGUF`。SHA256 必须是 `06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439`（639150592 字节）。哈希不符时 Relay 不启动它，检索只用关键词。

Relay 在第一次 `update_memory` 写入、`recall_memory` 或回填时才启动进程，权重 mmap，不传 `--no-mmap` 或 `--mlock`。默认空闲 5 分钟后退出，用 `ARIEL_PUCK_EMBEDDER_IDLE` 调整（例如 `3m`）。启动时限默认 30 秒，用 `ARIEL_PUCK_EMBEDDER_START_TIMEOUT` 调整。Mac mini 上冷启动实测约 5.3 秒。`update_memory` 最多等 2 秒；冷启动没结束就先收下原文，向量由后台那一次启动或回填补上。

`ARIEL_PUCK_EMBEDDER=off` 关闭嵌入。`http` 加上 loopback 的 `ARIEL_PUCK_EMBEDDER_URL` 表示进程已经由别人启动，Relay 不负责关掉它。公网容器不要放入模型，也不要把这个地址指到外网。
