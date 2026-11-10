# 0147：Puck 头像选项与 Grok Bot 对齐

- 状态：Implemented（2026-11-10）
- 日期：2026-11-10

## 已核对

1. 形状 id 与顺序、颜色 id 与顺序，和用户在线上 Grok Bot profile schema 里核对过的枚举一致。
2. 同一份形状名单也出现在非官方提取记录 `RongleCat/grok-bot-avatar-engine` 的 `data/extracted/picker-shapes.json`（自称来自 Grok Bot 0.78.1 客户端 `index.eager-common-DbK3Oi_r.js`，offset 171855，sha1 `12e00e138185`）。
3. 颜色 hex 取自同一仓库的 `data/extracted/color-table.json`（offset 170572，sha1 `0bdc0ca0ebd4`）。这不是线上实测，也不是 xAI 公布的色值。

## 未核对

1. 轮廓不是客户端的网格投影。公开材料里的几何是程序化 mesh，没有可直接引用的官方 SVG path。本实现只用一一对应的正面剪影，不能称为像素级一致。
2. 左侧菜单头像从 80px 改为 40px。没有找到可核对的 Grok Bot 侧栏头像像素。提取物里的 `compact-threshold` 只是常量 `48`，不能当成侧栏尺寸。行高仍是 96px，侧栏宽仍是 280px。
3. 需要的对照：Grok Bot 0.78.1 选择器的实拍，或客户端 bundle 里投影出的静止 SVG，以及侧栏头像的实测宽高。

## 行为

1. 去掉存在光环。
2. 选择器只提供上述 25 个形状和 11 个颜色 id。旧形状名和 `#rrggbb` 在读取时映射到新 id；已存的 hex 在用户改选颜色前仍按原值上色。
3. `image` 为 https URL 或 png/jpeg/webp data URI 时显示照片，改形状或颜色不会丢掉它。没有新增上传入口。
