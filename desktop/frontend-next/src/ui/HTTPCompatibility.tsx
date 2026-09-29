import { t } from "../i18n";

export function HTTPCompatibility({ kind, value, onChange, disabled }: {
  kind: string; value: boolean; onChange: (value: boolean) => void; disabled?: boolean;
}) {
  if (!["openai", "anthropic", "responses", "dashscope-responses"].includes(kind)) return null;
  return <label className="grow full">
    <span>{t("HTTP 连接协议")}</span>
    <select value={value ? "http1" : "auto"} disabled={disabled}
      data-action="provider.draft" data-value="http-protocol"
      onChange={event => onChange(event.target.value === "http1")}>
      <option value="auto">{t("自动协商（默认）")}</option>
      <option value="http1">HTTP/1.1</option>
    </select>
    <i className="tip">{t("遇到 HTTP/2 协议兼容问题时可尝试 HTTP/1.1。保存不会重发失败请求；正在运行的对话可能需要重新加载连接。")}</i>
  </label>;
}
