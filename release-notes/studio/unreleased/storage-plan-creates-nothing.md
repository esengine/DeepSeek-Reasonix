## 修复

- 修复在「设置 → 数据 → 存储」里更改位置时，每敲一个字符就在磁盘上留下一个半截目录的问题（比如输入 `D:\test\reasonix` 会留下 `r`、`re`、`rea` 等 8 个空目录）。现在预检只读：它在目标最近的已存在上级目录里验证可写、读取剩余空间，目录要等你点「开始迁移」后才创建（#14998）。

---
<!-- lang:en -->

## Fixed

- Typing a new location under Settings > Data > Storage no longer leaves a half-typed folder on disk for every keystroke (typing `D:\test\reasonix` used to leave `r`, `re`, `rea` and so on as eight empty folders). The check is now read-only: it proves the nearest existing parent is writable and reads its free space, and the folder is only created when you start the move (#14998).
