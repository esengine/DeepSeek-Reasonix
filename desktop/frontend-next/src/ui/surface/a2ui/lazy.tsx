import { lazy, Suspense, useState, type ComponentType } from "react";
import { Boundary } from "../../Boundary";
import { t } from "../../../i18n";

export function lazySurface<P extends object>(load: () => Promise<{ default: ComponentType<P> }>) {
  const loader = () => lazy(async () => {
    try { return await load(); }
    catch (error) { View = loader(); throw error; }
  });
  let View = loader();
  return function LazySurface(props: P) {
    const [attempt, setAttempt] = useState(0);
    return <Boundary retryKey={attempt} fallback={<button type="button" className="dismiss" onClick={() => setAttempt(v => v + 1)}>{t("common.failed_to_load_retry")}</button>}>
      <Suspense fallback={null}><View {...props} /></Suspense>
    </Boundary>;
  };
}
