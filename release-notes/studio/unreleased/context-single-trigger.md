## 变更

- 上下文压缩只有一个自动触发点：输入到模型窗口的一定比例，新配置默认从 85% 改为 90%，已有配置保持原值（7e1cdccb8）。

## 移除

- 移除按绝对大小提前压缩的上限，以及设置页里的“整理策略/阈值”和上下文面板里的拖动手柄（7e1cdccb8）。
- 折叠时不再省略保留下来的失败输出，也不再为此多发一次模型调用（7e1cdccb8）。

---
<!-- lang:en -->

## Changed

- Context compaction has a single automatic trigger, a share of the model window; the default for new configs moves from 85% to 90% and saved configs keep their value (7e1cdccb8).

## Removed

- Removed the absolute-size compaction limit, the threshold controls in Settings and the draggable fold handle in the context panel (7e1cdccb8).
- A fold no longer elides retained failure output and no longer makes an extra model call to choose which lines to keep (7e1cdccb8).
