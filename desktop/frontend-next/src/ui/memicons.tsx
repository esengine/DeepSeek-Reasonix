import { StudioIcon, type StudioIconName } from "./StudioIcon";

const ICONS: Record<string, StudioIconName> = {
  all: "grid", new: "spark", constraint: "lock", user: "user", feedback: "feedback",
  project: "folder", reference: "link", galaxy: "galaxy", archive: "archive",
  search: "search", importance: "chart", clock: "clock", uses: "repeat",
  loading: "loading", check: "check", edit: "edit", cross: "close", pin: "pin",
};

export function Icon({ name, size = 16 }: { name: string; size?: number }) {
  return <StudioIcon name={Object.hasOwn(ICONS, name) ? ICONS[name] : "bookmark"} className="memi" width={size} height={size} style={{ width: size, height: size }} />;
}

export function StateIcon({ state, size = 16 }: { state?: string; size?: number }) {
  return <StudioIcon name={state === "faded" ? "circleDashed" : state === "fuzzy" ? "fading" : "circle"} className="memi"
    width={size} height={size} style={{ width: size, height: size }} data-state={state ?? "clear"} />;
}
