import { useState } from "react";
import { ExtensionView } from "./ExtensionView";
import type { ExtensionSurface, ExtensionFormField } from "../../port/wire";
import { Markdown } from "../Markdown";
import { Sym } from "../Sym";

interface Props {
  ext: ExtensionSurface;
  onInvoke?: (name: string) => void;
  onSubmit?: (pluginId: string, surfaceId: string, values: Record<string, unknown>) => void;
}

// An extension publishes data, not markup — so severity arrives as its own
// word and gets the same left colour bar every other card in this UI uses.
function lvl(severity?: string): string | undefined {
  switch (severity) {
    case "error":
    case "critical":
      return "err";
    case "warn":
    case "warning":
      return "warn";
    case "ok":
    case "success":
      return "ok";
    default:
      return undefined;
  }
}

function Bar({ value }: { value: number }) {
  const pct = Math.max(0, Math.min(1, value)) * 100;
  return (
    <span className="extbar" role="progressbar" aria-valuenow={Math.round(pct)}>
      <i style={{ width: `${pct}%` }} />
    </span>
  );
}

// Kinds the host cannot draw are still worth showing as their raw field text:
// an extension built against a newer surface kind degrades to something the
// user can read instead of vanishing.
export function ExtensionCard({ ext, onInvoke, onSubmit }: Props) {
  const [values, setValues] = useState<Record<string, unknown>>(() => seed(ext.form?.fields));
  const [sent, setSent] = useState(false);

  const set = (key: string, v: unknown) => setValues((prev) => ({ ...prev, [key]: v }));
  const missing = (ext.form?.fields ?? []).filter((f) => f.required && empty(values[f.key]));

  return (
    <div className="call" data-k="extension">
      <div className="g">
        <Sym glyph="◈" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{ext.pluginId}</span>
          <span className="tag">{ext.kind}</span>
          {ext.card?.title && <span className="arg">{ext.card.title}</span>}
          {ext.form?.title && <span className="arg">{ext.form.title}</span>}
        </div>
        <div className="out">
          {/* A composed surface draws itself from primitives. It comes first
              because an extension that sent one meant it as the surface, not
              as a decoration on the fixed fields. */}
          {ext.view && <ExtensionView body={ext.view.body} onAction={onInvoke} />}

          {ext.status && (
            <div className="finds">
              <div className="find" data-lvl={lvl(ext.status.severity)}>
                <span className="t">{ext.status.label}</span>
                {ext.status.detail && <span className="why">{ext.status.detail}</span>}
                {ext.status.progress !== undefined && <Bar value={ext.status.progress} />}
              </div>
            </div>
          )}

          {ext.notification && (
            <div className="finds">
              <div className="find" data-lvl={lvl(ext.notification.severity)}>
                <span className="t">{ext.notification.title}</span>
                {ext.notification.body && <span className="why">{ext.notification.body}</span>}
              </div>
            </div>
          )}

          {ext.card && (
            <div className="extcard">
              {ext.card.markdown ? (
                <Markdown text={ext.card.markdown} />
              ) : (
                ext.card.text && <div className="exttext">{ext.card.text}</div>
              )}
              {ext.card.progress !== undefined && <Bar value={ext.card.progress} />}
              {!!ext.card.fields?.length && (
                <dl className="extkv">
                  {ext.card.fields.map((f) => (
                    <div key={f.key}>
                      <dt>{f.key}</dt>
                      <dd>{f.value}</dd>
                    </div>
                  ))}
                </dl>
              )}
              {!!ext.card.actions?.length && (
                <div className="extacts">
                  {ext.card.actions.map((a) => (
                    <button
                      data-action="extensions.invoke"
                      key={a.actionId}
                      className="btn"
                      onClick={() => onInvoke?.(`/${ext.pluginId}:${a.actionId}`)}
                    >
                      {a.label || a.actionId}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          {ext.form && (
            <div className="extform" data-sealed={sent ? "" : undefined}>
              {ext.form.message && <div className="exttext">{ext.form.message}</div>}
              {ext.form.fields.map((f) => (
                <Field key={f.key} f={f} value={values[f.key]} sealed={sent} onChange={(v) => set(f.key, v)} />
              ))}
              {!sent && (
                <div className="extfoot">
                  <button
                    data-action="extensions.submit"
                    className="btn"
                    data-primary
                    disabled={missing.length > 0}
                    onClick={() => {
                      setSent(true);
                      onSubmit?.(ext.pluginId, ext.surfaceId, values);
                    }}
                  >
                    {missing.length ? `提交（还差 ${missing.length} 项）` : "提交"}
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function Field({
  f,
  value,
  sealed,
  onChange,
}: {
  f: ExtensionFormField;
  value: unknown;
  sealed: boolean;
  onChange: (v: unknown) => void;
}) {
  const label = f.label || f.key;
  const multi = f.kind === "multiselect";

  if (f.kind === "select" || multi) {
    const picked = multi ? asList(value) : value === undefined ? [] : [String(value)];
    return (
      <div className="extfield">
        <div className="extlabel">
          {label}
          {f.required && <i className="req">*</i>}
        </div>
        <div className="opts">
          {(f.options ?? []).map((opt) => (
            <button
              key={opt}
              className="opt"
              data-multi={multi ? "" : undefined}
              data-on={picked.includes(opt) ? "" : undefined}
              disabled={sealed}
              onClick={() => {
                if (sealed) return;
                if (!multi) return onChange(opt);
                onChange(picked.includes(opt) ? picked.filter((p) => p !== opt) : [...picked, opt]);
              }}
            >
              <span className="mark" />
              <span className="txt">
                <span className="lb">{opt}</span>
              </span>
            </button>
          ))}
        </div>
      </div>
    );
  }

  if (f.kind === "confirm") {
    return (
      <div className="extfield">
        <div className="opts">
          <button
            className="opt"
            data-multi=""
            data-on={value === true ? "" : undefined}
            disabled={sealed}
            onClick={() => !sealed && onChange(value !== true)}
          >
            <span className="mark" />
            <span className="txt">
              <span className="lb">
                {label}
                {f.required && <i className="req">*</i>}
              </span>
            </span>
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="extfield">
      <div className="extlabel">
        {label}
        {f.required && <i className="req">*</i>}
      </div>
      <input
        value={value === undefined || value === null ? "" : String(value)}
        readOnly={sealed}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  );
}

// A declared default is the extension's answer until the user changes it, so
// the form opens pre-filled rather than empty.
function seed(fields?: ExtensionFormField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields ?? []) {
    if (f.default !== undefined) out[f.key] = f.default;
    else if (f.kind === "multiselect") out[f.key] = [];
    else if (f.kind === "confirm") out[f.key] = false;
  }
  return out;
}

function asList(v: unknown): string[] {
  return Array.isArray(v) ? v.map(String) : [];
}

function empty(v: unknown): boolean {
  if (v === undefined || v === null) return true;
  if (Array.isArray(v)) return v.length === 0;
  if (typeof v === "string") return v.trim() === "";
  if (typeof v === "boolean") return v === false;
  return false;
}
