import { useEffect, useState } from "react";
import { onLanguageChange } from "../../../i18n/index.js";

export function useLanguageReload(): number {
  const [version, setVersion] = useState(0);
  useEffect(() => onLanguageChange(() => setVersion((v) => v + 1)), []);
  return version;
}
