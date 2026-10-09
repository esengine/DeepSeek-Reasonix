// Refreshes the contributor list the desktop app shows, as logins only: no
// avatars, so the app never fetches from GitHub to draw it. The repository owner
// is left out. Run at release time.

import { execFileSync } from "node:child_process";
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defaultRepository, isBotAccount } from "./release-credits.mjs";

export const outputPath = fileURLToPath(new URL("../desktop/frontend-next/src/data/contributors.json", import.meta.url));

export function ownerOf(repository) {
  return repository.split("/")[0];
}

export function loginsFrom(rows, owner = "") {
  const skip = owner.toLowerCase();
  return rows
    .filter((row) => row && typeof row.login === "string" && !isBotAccount(row.login, row.type) && row.login.toLowerCase() !== skip)
    .sort((a, b) => (b.contributions ?? 0) - (a.contributions ?? 0) || a.login.localeCompare(b.login))
    .map((row) => row.login);
}

export function render(logins) {
  return `${JSON.stringify(logins, null, 2)}\n`;
}

function fetchRows(repository) {
  const out = execFileSync("gh", ["api", `repos/${repository}/contributors?per_page=100`, "--paginate", "--slurp"], {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  return JSON.parse(out).flat();
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const repository = process.argv[2] ?? defaultRepository;
  const logins = loginsFrom(fetchRows(repository), ownerOf(repository));
  writeFileSync(outputPath, render(logins));
  console.log(`wrote ${logins.length} contributors to ${outputPath}`);
}
