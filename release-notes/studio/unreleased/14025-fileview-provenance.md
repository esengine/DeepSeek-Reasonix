## 修复

- File change protection records the complete returned read; pages and oversized
  reads preserve the earlier guard. (#14025)
- 文件变更保护现在记录完整读取实际返回的内容，避免二次读取误记录后续文件或
  编辑器版本；分页和超限读取会保留已有的过期覆盖保护。 (#14025)
- After a partial read, read the file in full before a whole-file overwrite, or
  use a targeted edit. (#14025)
- 首次只读取一部分内容后，完整覆盖前需要完整重读，或改用局部编辑。 (#14025)
