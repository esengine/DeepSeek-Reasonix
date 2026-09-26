// Every shape here was read off real tool results, not inferred: a search hit
// is "path:line:text", a context line repeats the path with a dash, a summary
// is "path: N matches", ls is "name<TAB>bytes". Anything that does not parse
// cleanly falls back to the terminal block rather than being forced into a
// list — a wrong list reads as authoritative, a raw block only reads as raw.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { useStartsOpen } from "../../state/foldpref";
import { bytes } from "../../i18n/format";
import type { Bound } from "../../port/wire";
import { splitPath } from "../args";

const MATCH = /^(.+?):(\d+):([\s\S]*)$/;
// ls prints a directory as "name/" with no size and a file as "name<TAB>bytes".
const LS_DIR = /^(.+\/)$/;
const LS_FILE = /^(.+)\t(\d+)$/;

// Below this share of lines fitting the shape, the output is something else.
const CONFIDENT = 0.8;

export interface Hit {
  path: string;
  line: string;
  text: string;
  more: string[];
}

export function parseHits(out: string): { hits: Hit[]; note: string } | null {
  const all = out.split("\n").filter((l) => l.trim());
  // grep appends its own truncation and timeout notes. They are the tool
  // telling you the result is partial, not stray output — pull them out before
  // judging the shape, and keep them to show.
  const note = all.filter((l) => TRAILER.test(l)).join(" ");
  const lines = all.filter((l) => !TRAILER.test(l));
  if (lines.length === 0) return null;
  const hits: Hit[] = [];
  let loose = 0;
  for (const line of lines) {
    const m = MATCH.exec(line);
    if (m) {
      hits.push({ path: m[1], line: m[2], text: m[3].trim(), more: [] });
      continue;
    }
    const cur = hits[hits.length - 1];
    // A context line repeats the hit's own path, so strip it by literal rather
    // than by a pattern that a path containing "-12-" would fool.
    if (cur && line.startsWith(cur.path)) {
      cur.more.push(line.slice(cur.path.length).replace(/^[-:]\d+[-:]/, "").trim());
      continue;
    }
    loose++;
  }
  return hits.length && loose / lines.length <= 1 - CONFIDENT ? { hits, note } : null;
}

export interface Row {
  name: string;
  n: string;
}

export function parseRows(out: string, re: RegExp): Row[] | null {
  const lines = out.split("\n").filter((l) => l.trim());
  if (lines.length === 0) return null;
  const rows = lines.map((l) => re.exec(l)).filter((m): m is RegExpExecArray => m !== null);
  if (rows.length / lines.length < CONFIDENT) return null;
  // A shape with no count still makes a manifest; the column just stays empty.
  return rows.map((m) => ({ name: m[1], n: m[2] ?? "" }));
}

export function parseListing(out: string): Row[] | null {
  const lines = out.split("\n").filter((l) => l.trim());
  if (lines.length === 0) return null;
  const rows: Row[] = [];
  for (const l of lines) {
    const f = LS_FILE.exec(l);
    if (f) {
      rows.push({ name: f[1], n: f[2] });
      continue;
    }
    const d = LS_DIR.exec(l);
    if (d) rows.push({ name: d[1], n: "" });
  }
  return rows.length / lines.length < CONFIDENT ? null : rows;
}

// A manifest is read-only where the spec draws it. Only a merged read run makes
// its rows pickable, because collapsing the cards would otherwise put the file
// contents out of reach entirely.
export function Peek({
  rows,
  unit,
  onPick,
  at,
}: {
  rows: { name: string; n: string }[];
  unit: string;
  onPick?: (i: number) => void;
  at?: number;
}) {
  return (
    <div className="peek">
      {rows.map((r, i) => {
        const [dir, name] = splitPath(r.name);
        const body = (
          <>
            <span>
              {dir && <span className="d">{dir}</span>}
              {name}
            </span>
            {r.n && (
              <span className="n">
                {r.n}
                {unit && ` ${unit}`}
              </span>
            )}
          </>
        );
        return onPick ? (
          <button className="row" key={i} data-on={at === i ? "" : undefined} onClick={() => onPick(i)}>
            {body}
          </button>
        ) : (
          <div className="row" key={i}>
            {body}
          </div>
        );
      })}
    </div>
  );
}

// One result row: what it is, where it came from, what it said. A grep hit and a
// web search result are the same shape — a claim with a source — so the spec
// draws them with one set of elements rather than two lookalike lists.
export interface Row3 {
  t: string;
  u: string;
  q: string[];
}

export const hitRows = (hits: Hit[]): Row3[] =>
  hits.map((h) => {
    const [dir, name] = splitPath(h.path);
    return { t: name, u: `${dir}${name}:${h.line}`, q: [h.text, ...h.more] };
  });

export function Hits({ rows, note }: { rows: Row3[]; note?: string }) {
  return (
    <div className="hits">
      {rows.map((r, i) => (
        <div className="hit-row" key={i}>
          <div className="i">{i + 1}</div>
          {/* .t/.u/.q carry margin-top and ellipsis but no display in the
              spec's CSS, so they were block elements in the prototype. */}
          <div>
            <div className="t">{r.t}</div>
            {r.u && (
              <div className="u" title={r.u}>
                {r.u}
              </div>
            )}
            {/* A provider that encrypts result bodies leaves nothing to quote,
                and an empty .q still draws the spec's quote rule. */}
            {r.q.some(Boolean) && (
              <div className="q">
                {r.q[0]}
                {r.q.slice(1).map((m, k) => (
                  <div key={k}>{m}</div>
                ))}
              </div>
            )}
          </div>
        </div>
      ))}
      {note && (
        <div className="hit-row">
          <div className="i" />
          <div className="u">{note}</div>
        </div>
      )}
    </div>
  );
}

// The terminal fills in line by line: the first few land on the beat, the rest
// tighten up so a long block still finishes inside SETTLED.
const HEAD = 5;
const BEAT = 34;
const TIGHT = 8;
const SETTLED = 700;
// Past this, splitting into one node per line costs more than the entrance is
// worth, so the block arrives whole.
const SPLIT_MAX = 300;

// A progress meter redraws by returning to column zero, and a browser draws a
// carriage return as a space, so each line is resolved the way a terminal
// would leave it: every frame overwrites the start of the one before.
function overstrike(text: string): string {
  if (!text.includes("\r")) return text;
  return text
    .split("\n")
    .map((line) => {
      const frames = (line.endsWith("\r") ? line.slice(0, -1) : line).split("\r");
      return frames.reduce((shown, frame) => frame + shown.slice(frame.length));
    })
    .join("\n");
}

export function Term({ text: raw, one }: { text: string; one?: boolean }) {
  const text = overstrike(raw);
  const lines = text.split("\n");
  if (lines.length > SPLIT_MAX) return <pre className="term">{text}</pre>;
  return (
    <pre className="term" data-one={one ? "" : undefined}>
      {lines.map((l, i) => (
        <span
          className="term-l"
          key={i}
          style={{ animationDelay: `${Math.min(i < HEAD ? i * BEAT : HEAD * BEAT + (i - HEAD) * TIGHT, SETTLED)}ms` }}
        >
          {l}
          {i < lines.length - 1 ? "\n" : ""}
        </span>
      ))}
    </pre>
  );
}

// A provider-run search returns the listing the kernel formats for the model:
// "- **title**" and, indented under it, "<url>". A result can arrive with no URL
// at all, so the title alone still counts as a row.
const SEARCH_TITLE = /^-\s+\*\*(.+?)\*\*\s*$/;
const SEARCH_URL = /^\s+<(\S+)>\s*$/;

export function parseSearchResults(out: string): Row3[] | null {
  const lines = out.split("\n").filter((l) => l.trim());
  if (lines.length === 0) return null;
  const rows: Row3[] = [];
  let loose = 0;
  for (const line of lines) {
    const title = SEARCH_TITLE.exec(line);
    if (title) {
      rows.push({ t: title[1], u: "", q: [] });
      continue;
    }
    const url = SEARCH_URL.exec(line);
    const cur = rows[rows.length - 1];
    if (url && cur && !cur.u) {
      cur.u = url[1];
      continue;
    }
    loose++;
  }
  return rows.length && loose / lines.length <= 1 - CONFIDENT ? rows : null;
}

// glob returns one path per line. Requiring a separator keeps a one-line error
// message from being dressed up as a manifest of one file.
export const PATH = /^\s*([^\s]*[/\\][^\s]*)\s*$/;
const TRAILER = /^\.\.\. \(/;

export function ToolOutput({ name, text, bound, id }: { name: string; text: string; bound?: Bound; id?: string }) {
  return (
    <>
      <Clip id={id} deps={text}>
        {shapeFor(name, text)}
      </Clip>
      <BoundNote bound={bound} />
    </>
  );
}

// A directory listing and a glob result are both manifests, which is what .peek
// is for. grep prints "path:line:text" and gets the excerpt list. Everything
// else — a shell run, an MCP result, a listing that did not parse — is a block.
function shapeFor(name: string, text: string): ReactNode {
  if (name === "ls") {
    const rows = parseListing(text);
    if (rows) return <Peek rows={rows} unit="B" />;
  }
  if (name === "glob") {
    const rows = parseRows(text, PATH);
    if (rows) return <Peek rows={rows} unit="" />;
  }
  if (name === "grep") {
    const found = parseHits(text);
    if (found) return <Hits rows={hitRows(found.hits)} note={found.note} />;
  }
  if (name === "web_search") {
    const rows = parseSearchResults(text);
    if (rows) return <Hits rows={rows} />;
  }
  // A surface exists to cut a block of machine text into the page. One line is
  // not a block: it costs the same 42px of padded ground as forty do, and a
  // settled call whose whole output is "ok" then weighs what a test run does.
  return text.includes("\n") ? <Term text={text} /> : <Term text={text} one />;
}

// One visual budget for every shape above, carried by CSS rather than counted
// here. Three line thresholds only made a manifest, a block and a hit list
// disagree about what "long" means, and each one dropped rows the model had
// actually been given. Clipping keeps the content scannable and complete.
const opened = new Map<string, boolean>();

function Clip({ id, deps, children }: { id?: string; deps?: unknown; children: ReactNode }) {
  const start = useStartsOpen("output");
  const [touched, setOpen] = useState(() => (id ? opened.get(id) : undefined));
  const open = touched ?? start;
  const [over, setOver] = useState(false);
  const body = useRef<HTMLDivElement>(null);
  // Measuring reads scrollHeight, which forces layout — so it keys off the
  // content and the clip state, not every render the turn above it causes.
  useEffect(() => {
    const el = body.current;
    if (!el) return;
    const measure = () => setOver(el.scrollHeight > el.clientHeight + 1);
    // Off the commit, onto the next frame. Opening a long transcript mounts
    // hundreds of these at once, and reading scrollHeight inside the commit
    // makes each card force a layout of the whole document — the single
    // largest cost in switching to a long session on a slow machine.
    const raf = requestAnimationFrame(() => {
      // Collapsing now takes time, and mid-transition this measures an intermediate
      // height — at that instant the test flips to "not overflowing" and pulls the
      // expand button. While one is running, wait for it to settle.
      if (!el.getAnimations().length) measure();
    });
    el.addEventListener("transitionend", measure);
    return () => {
      cancelAnimationFrame(raf);
      el.removeEventListener("transitionend", measure);
    };
  }, [deps, open]);
  const toggle = () => {
    const next = !open;
    setOpen(next);
    // Keyed by call, so reopening a card the user already expanded does not ask
    // them to expand it again.
    if (id) opened.set(id, next);
  };
  return (
    <div className="out-clip" data-open={open ? "" : undefined} data-over={over ? "" : undefined}>
      <div className="out-body" ref={body}>
        {children}
      </div>
      {(over || open) && (
        <button className="out-more" onClick={toggle}>
          {open ? t("收起") : t("展开全部")}
        </button>
      )}
    </div>
  );
}

// The three outcomes are not the same news, and a card that renders them alike
// is why "folded" and "the model never saw this" looked identical. Only
// truncation is a warning; the other two say where the rest is.
function BoundNote({ bound }: { bound?: Bound }) {
  if (!bound) return null;
  if (bound.kind === "spilled") {
    return (
      <div className="bound" title={bound.path}>
        {t("完整输出已保存（{lines} 行 · {size}），模型按需读取", {
          lines: bound.lines ?? 0,
          size: bytes(bound.bytes ?? 0),
        })}
      </div>
    );
  }
  if (bound.kind === "windowed") {
    return <div className="bound">{t("已显示开头部分，模型可继续往下读")}</div>;
  }
  return (
    <div className="bound bad">
      {t("模型只收到 {kept}，共 {size} — 其余未进入上下文", {
        kept: bytes(bound.keptBytes ?? 0),
        size: bytes(bound.bytes ?? 0),
      })}
    </div>
  );
}
