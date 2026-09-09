import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

// The soak runs the production frontend against browser mocks. These source
// trees cannot enter that build; native/backend behavior has separate gates.
const knownIndependent = /^(?:internal\/|cmd\/|sdk\/|site\/|release-notes\/|docs\/[^\n]*\.md$|desktop\/(?:[^/]+\.go$|cmd\/|internal\/))/;
export function memoryAffected(files) {
  return files.some(file => {
    if (file.startsWith("desktop/frontend/") || file === ".github/workflows/app-memory.yml") return true;
    if (knownIndependent.test(file) || /^[^/]+\.md$/.test(file)) return false;
    return true; // Unknown dependency or workflow changes fail closed.
  });
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const files = readFileSync(process.argv[2], "utf8").split("\0").filter(Boolean);
  console.log(`run=${memoryAffected(files)}`);
}
