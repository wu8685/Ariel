# 0128 FEATURE_DISABLED 上下文文案

- 状态：Approved
- 日期：2026-11-09

## 规格

- `FEATURE_DISABLED` 不再固定显示“文件引用功能尚未启用”。
- error formatter 接受可选 method，按 method family 映射：文件引用、workspace 文件浏览、文档图片、模型设置、消息编辑、队列、交互审批与通用 fallback。
- 未知 method 或缺少 method 时显示“这项功能尚未启用。”，不透传可能不友好的原始 code。
- 其他错误码既有文案不变。

## 验收

- 单元测试覆盖各 method family 与未知 fallback。
- production build 与 App tests 通过。
