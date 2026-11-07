## 修复

- 修复设置里「思考过程」选成「展开」后，模型思考时仍然只显示最新六行、要等整段思考结束才一下子全部显示的问题：选「展开」时，运行中的思考块现在随内容一直完整展开；「进行中展开」仍是最新六行的滚动窗口（#15013）。

---
<!-- lang:en -->

## Fixed

- With Thinking set to "Open", a thought still showed only its latest six lines while the model was thinking and then jumped to the full text when it finished. In "Open" the running thought now grows with its text; "Open while running" keeps the six-line rolling window (#15013).
