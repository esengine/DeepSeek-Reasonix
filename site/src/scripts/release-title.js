// Release-hero classification: a release whose title is exactly its version
// string ("Reasonix 1.17.14") is a patch release and gets the compact hero.
// Anything else — including titles that merely START with the version, like
// "Reasonix 1.18.0 — Better agents" — is thematic and keeps the full hero.
// Exact, normalized comparison only (no prefix regex).
export function isThematicTitle(titleEn, version) {
  const title = String(titleEn || "").trim().toLowerCase().replace(/\s+/g, " ");
  const v = String(version || "").trim().toLowerCase();
  return title !== `reasonix ${v}` && title !== `reasonix v${v}`;
}
