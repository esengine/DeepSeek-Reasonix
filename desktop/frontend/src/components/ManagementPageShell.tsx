import { useLayoutEffect, useRef, type ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { useManagementT } from "../lib/managementLocale";
import "./ManagementPageShell.css";

export function ManagementPageShell({ title, description, actions, navigation, contentRef, children, active = true, onBack, className = "" }: {
  title: string; description?: string; actions?: ReactNode; navigation?: ReactNode; children: ReactNode;
  contentRef?: React.RefObject<HTMLElement | null>;
  active?: boolean; onBack: () => void; className?: string;
}) {
  const t = useManagementT();
  const backRef = useRef<HTMLButtonElement>(null);
  useLayoutEffect(() => { if (active) backRef.current?.focus({ preventScroll: true }); }, [active]);
  const back = <button ref={backRef} className="management-screen__back" type="button" onClick={onBack}><ArrowLeft size={18} />{t("back")}</button>;
  return <section className={`management-screen ${className}`} hidden={!active} inert={!active} aria-label={title}>
    <header className="management-screen__chrome" aria-hidden="true" />
    {navigation ? <div className="settings-center"><aside className="settings-screen__sidebar">{back}{navigation}</aside><main ref={contentRef} className="settings-center__content">{children}</main></div> : <>
      <div className="management-screen__top">{back}<div className="management-screen__heading"><h1>{title}</h1>{description && <p>{description}</p>}</div><div className="management-screen__actions">{actions}</div></div>
      <main className="management-screen__content">{children}</main>
    </>}
  </section>;
}
