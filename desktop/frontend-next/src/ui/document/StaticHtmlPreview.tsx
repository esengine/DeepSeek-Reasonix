import { useMemo } from "react";
import { t } from "../../i18n";
import { staticDocument } from "./staticDocument";

export default function StaticHtmlPreview({ text }: { text: string }) {
  const document = useMemo(() => staticDocument(text), [text]);
  return <>
    <p className="document-notice">{t("chat.static_preview_scripts_and_external")}</p>
    <iframe className="document-html" title={t("chat.static_html_preview")} sandbox="" srcDoc={document} />
  </>;
}
