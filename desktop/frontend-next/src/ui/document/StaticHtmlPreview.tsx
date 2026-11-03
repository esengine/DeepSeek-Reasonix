import { useMemo } from "react";
import { t } from "../../i18n";
import { staticDocument } from "./staticDocument";

export default function StaticHtmlPreview({ text }: { text: string }) {
  const document = useMemo(() => staticDocument(text), [text]);
  return <>
    <p className="document-notice">{t("staticHtmlPreview.staticPreviewScriptsAndExternal")}</p>
    <iframe className="document-html" title={t("staticHtmlPreview.staticHTMLPreview")} sandbox="" srcDoc={document} />
  </>;
}
