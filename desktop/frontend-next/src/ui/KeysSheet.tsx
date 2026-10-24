import { t } from "../i18n";
import { chord } from "./keys";
import { Group } from "./Group";

/** One row: the chord as this platform writes it, and what it does. */
const KEYS: Array<[string, string]> = [
  [chord("F"), "在对话里查找"],
  [chord("K"), "搜索会话"],
  [chord("1"), "显示对话"],
  [chord("2"), "显示运行分析"],
  [chord("3"), "显示工作台"],
  [`${chord("↑")} / ${chord("↓")}`, "在提问之间移动"],
  [chord("Shift+E"), "优化提示词"],
  [chord("Shift+F"), "工作台全屏"],
  [chord("Shift+B"), "工作台加宽"],
  [`${chord("-")} / ${chord("+")} / ${chord("0")}`, "缩小 / 放大 / 复原界面"],
  [chord("Enter"), "发送（发送键设为修饰键 + Enter 时）"],
  ["Enter / Shift+Enter", "发送，或用 Shift 换行"],
  ["Esc", "关闭当前弹层"],
  ["← / → · Home", "聚焦分隔条后调宽或用 Home 复位"],
];

/** Every chord the window answers to, on one screen.
 *
 *  Nothing here sets anything: it is a reference, so it carries no scope and no
 *  apply semantics. It lives beside the appearance settings because that is
 *  where someone goes when the window does not behave the way they expect. */
export function KeysSheet() {
  return (
    <Group
      id="keys"
      title={t("快捷键")}
      hint={t("下列按键在窗口中生效；标着 ⌘ 的地方，Windows 与 Linux 上是 Ctrl。")}
    >
      {KEYS.map(([keys, what]) => (
        <div className="lrow keys-row" key={keys + what}>
          <span className="ds"><kbd>{keys}</kbd></span>
          <span className="tx"><span className="lb">{t(what)}</span></span>
        </div>
      ))}
    </Group>
  );
}
