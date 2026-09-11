import { Copy as RestoreIcon, Minus, Square, X } from "lucide-react";

export function WindowsWindowControls({ maximised, onMinimize, onToggleMaximize, onClose }: {
  maximised: boolean;
  onMinimize: () => void;
  onToggleMaximize: () => void;
  onClose: () => void;
}) {
  return (
    <div className="windows-window-controls" aria-label="Window controls">
      <button className="windows-window-control windows-window-control--minimize" type="button" aria-label="Minimize window" title="Minimize" onClick={onMinimize}>
        <Minus size={13} strokeWidth={1.9} />
      </button>
      <button className="windows-window-control windows-window-control--maximize" type="button" aria-label="Maximize or restore window" aria-pressed={maximised} title={maximised ? "Restore" : "Maximize"} onClick={onToggleMaximize}>
        {maximised ? <RestoreIcon size={12} strokeWidth={1.75} /> : <Square size={11} strokeWidth={1.8} />}
      </button>
      <button className="windows-window-control windows-window-control--close" type="button" aria-label="Close window" title="Close" onClick={onClose}>
        <X size={13} strokeWidth={1.9} />
      </button>
    </div>
  );
}
