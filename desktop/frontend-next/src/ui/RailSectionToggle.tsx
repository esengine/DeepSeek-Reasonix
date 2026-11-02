import { StudioIcon } from "./StudioIcon";

/** One disclosure target for a sidebar section's title and arrow. */
export function RailSectionToggle({ title, label, folded, onToggle, section }: {
  title: string;
  label: string;
  folded: boolean;
  onToggle: () => void;
  section: "team" | "projects";
}) {
  const isTeam = section === "team";
  return <h2 className="rail-section-heading">
    <button type="button" className="rail-section-toggle" data-action={isTeam ? "team.fold" : "workspace.section-fold"}
      aria-expanded={!folded} aria-label={label} title={label} onClick={onToggle}>
      <span>{title}</span><StudioIcon name={folded ? "chevron" : "down"} />
    </button>
  </h2>;
}
