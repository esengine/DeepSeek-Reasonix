import type { ReactNode } from "react";

export function SectionCard({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="prefs-card">
      <header>
        <h3>{title}</h3>
        {hint && <p>{hint}</p>}
      </header>
      {children}
    </section>
  );
}
