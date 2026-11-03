## 修复

- 修复在 1.x 里用过的会话提示「该待送达由更高版本写入，当前版本只能读取」的问题：队列里的消息既不能发送也不能删除（#14966）。
- 现在能正常读写 1.x 留下的待送达队列。带着 1.x 图片的消息会停住并说明原因，不会去掉图片照发（#14966）。

---
<!-- lang:en -->

## Fixed

- Sessions used in 1.x no longer show "This queue was written by a newer version and is read-only here" (#14966).
- The queue 1.x leaves behind now reads and writes normally. An entry queued with 1.x images is held with a stated reason, not sent without them (#14966).
