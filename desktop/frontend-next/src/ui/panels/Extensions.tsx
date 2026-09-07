import type { ExtensionSurface } from "../../port/wire";
import { t } from "../../i18n";
import { SlottedView } from "../SlottedView";

// Standing surfaces an extension published. They live here rather than in the
// transcript because they describe a state that is still true — a watcher, a
// sync, a connection — which scrolling away would hide.
export function Extensions({
  panels,
  views = [],
  onInvoke,
  onMove,
}: {
  panels: ExtensionSurface[];
  // Composed views that landed here: either the user put them here, or nobody
  // named a place they could be put. The rail is where a standing surface goes
  // when no one said otherwise.
  views?: ExtensionSurface[];
  onInvoke: (name: string) => void;
  onMove?: (ext: ExtensionSurface, slot: string) => void;
}) {
  if (panels.length === 0 && views.length === 0) return null;
  return (
    <div className="block" data-b="extpanels">
      <div className="lbl">
        {t("扩展")}<span className="c">{panels.length + views.length}</span>
      </div>
      {views.map((v) => (
        <div className="extpanel" key={`${v.pluginId}:${v.surfaceId}`}>
          <SlottedView
            ext={v}
            onAction={onInvoke}
            onMove={(slot) => onMove?.(v, slot)}
          />
        </div>
      ))}
      {panels.map((p) => {
        const panel = p.panel;
        if (!panel) return null;
        return (
          <div className="extpanel" key={`${p.pluginId}:${p.surfaceId}`}>
            <div className="extpanel-hd">
              <span className="nm">{panel.title || p.pluginId}</span>
              {panel.title && <span className="src">{p.pluginId}</span>}
            </div>
            {panel.text && <div className="extpanel-tx">{panel.text}</div>}
            {panel.progress !== undefined && (
              <span className="extbar" role="progressbar" aria-valuenow={Math.round(panel.progress * 100)}>
                <i style={{ width: `${Math.max(0, Math.min(1, panel.progress)) * 100}%` }} />
              </span>
            )}
            {!!panel.fields?.length && (
              <dl className="extkv">
                {panel.fields.map((f) => (
                  <div key={f.key}>
                    <dt>{f.key}</dt>
                    <dd>{f.value}</dd>
                  </div>
                ))}
              </dl>
            )}
            {!!panel.actions?.length && (
              <div className="extacts">
                {panel.actions.map((a) => (
                  <button
                    key={a.actionId}
                    className="btn"
                  data-action="extensions.invoke"
                    onClick={() => onInvoke(`/${p.pluginId}:${a.actionId}`)}
                  >
                    {a.label || a.actionId}
                  </button>
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
