## 修复

- 上下文已超过自动压缩线时点“立即压缩”，只要能腾出空间就会压缩，不再提示“无需压缩”（cee09e76b）。

---
<!-- lang:en -->

## Fixed

- Compacting by hand above the automatic trigger now folds whenever it frees space, instead of saying there is nothing to compact (cee09e76b).
