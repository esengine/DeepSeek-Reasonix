import assert from "node:assert/strict";
import { en } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

assert.equal(en["notice.deliveryIncompleteTitle"], "Task is not complete");
assert.match(en["notice.deliveryIncompleteBody"], /task work or checks that are incomplete/);
assert.equal(en["notice.deliveryIncompleteContinue"], "Continue checks");
assert.equal(en["notice.deliveryRequirementTask"], "remaining implementation work");

assert.equal(zh["notice.deliveryIncompleteTitle"], "任务尚未完成");
assert.match(zh["notice.deliveryIncompleteBody"], /任务或检查未完成/);
assert.equal(zh["notice.deliveryIncompleteContinue"], "继续检查");
assert.equal(zh["notice.deliveryRequirementTask"], "剩余实施工作");

assert.equal(zhTW["notice.deliveryIncompleteTitle"], "任務尚未完成");
assert.match(zhTW["notice.deliveryIncompleteBody"], /任務或檢查未完成/);
assert.equal(zhTW["notice.deliveryIncompleteContinue"], "繼續檢查");
assert.equal(zhTW["notice.deliveryRequirementTask"], "剩餘實作工作");

console.log("  PASS  final readiness recovery copy is aligned across locales");
