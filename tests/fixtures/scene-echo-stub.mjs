import { appendFileSync } from "node:fs";

const out = process.env.SCENE_ECHO_OUT;
if (!out) {
  process.stderr.write("SCENE_ECHO_OUT not set\n");
  process.exit(2);
}

let buf = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => {
  buf += chunk;
  let nl = buf.indexOf("\n");
  while (nl >= 0) {
    const line = buf.slice(0, nl);
    buf = buf.slice(nl + 1);
    if (line.trim().length > 0) appendFileSync(out, `${line}\n`);
    nl = buf.indexOf("\n");
  }
});
process.stdin.on("end", () => {
  if (buf.trim().length > 0) appendFileSync(out, `${buf}\n`);
  process.exit(0);
});
