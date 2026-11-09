## 修复

- 修复技能目录里的文档文件（比如 `skills/某技能/README.md`、`skills/某技能/子目录/README.md`）被误报成"没有描述，会加载但不会出现在技能索引里"的警告弹窗：这些嵌套的文件本来就不会被当成技能，现在只对真正保留下来的技能说这句话（#15045）。

---
<!-- lang:en -->

## Fixed

- Documentation files inside a skill folder, such as `skills/some-skill/README.md` or one a level deeper, no longer raise the warning that a skill "has no description: it will load but won't appear in the skills index". Such nested files are never loaded as skills; the warning now appears only for a skill that is kept (#15045).
