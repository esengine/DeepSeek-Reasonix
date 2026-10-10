import { t } from "../i18n";
import type { LegacySkip, LegacySkipReason } from "../port/hub";
import { CopyButton } from "./CopyButton";

const REASON: Record<LegacySkipReason, string> = {
  too_large: "体积超过读取上限",
  unreadable_format: "不是本版本能读取的会话格式",
  schema_unsupported: "来自本版本尚不支持的存储版本",
  permission: "没有读取权限",
  corrupt: "文件已损坏",
  copy_failed: "复制到新位置时失败",
};

export function LegacySkips({ skipped }: { skipped: LegacySkip[] }) {
  if (skipped.length === 0) return null;
  return (
    <details className="legacy-skips">
      <summary>{t("有 {n} 个会话没有导入", { n: skipped.length })}</summary>
      <span className="hint">{t("这些文件仍在原位置，没有被移动或删除。可复制路径自行处理，或修复后重新导入。")}</span>
      {skipped.map((skip) => (
        <div className="item" key={skip.path}>
          <div className="l">
            <b>{skip.name}</b>
            <span className="hint">{t(REASON[skip.reason] ?? REASON.copy_failed)}</span>
            <span className="p">{skip.path}</span>
            <CopyButton text={skip.path} label={t("复制路径")} ariaLabel={t("复制路径：{name}", { name: skip.name })} />
          </div>
        </div>
      ))}
    </details>
  );
}
