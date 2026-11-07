## 修复

- 修复 2.58.0 里，开着「会话行快捷按钮」时，侧栏每一行会话都一直显示一个置顶图标（不管有没有置顶）：现在这个按钮只在鼠标移上去或键盘聚焦时才出现；已置顶的会话在没悬停时会一直显示一个小图钉，方便一眼看出（#15012）。
- 修复输入框右下角的模型选单里，模型名字较长时，右边的「读图」标签被挤成两行竖着排的问题；现在标签始终在一行里完整显示（#15012）。

---
<!-- lang:en -->

## Fixed

- With "Quick buttons on session rows" turned on, every session row in the sidebar showed a pin icon all the time, pinned or not (2.58.0). The button now appears only when the pointer is over the row or it has keyboard focus, and a pinned session keeps a small pin on the row while nothing is hovered (#15012).
- In the model menu next to the message box, the "reads images" tag was squeezed into two stacked characters beside a long model name; it now stays whole on one line (#15012).
