import { useEffect, useRef, useState, type PointerEvent } from "react";
import { useT } from "../lib/i18n";
import type { WireApproval } from "../lib/types";

export function ApprovalModal({
  approval,
  onAnswer,
}: {
  approval: WireApproval;
  onAnswer: (allow: boolean, session: boolean) => void;
}) {
  const t = useT();
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [dragging, setDragging] = useState(false);
  const drag = useRef<{ x: number; y: number; ox: number; oy: number } | null>(null);

  useEffect(() => {
    setOffset({ x: 0, y: 0 });
  }, [approval.id]);

  const startDrag = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { x: event.clientX, y: event.clientY, ox: offset.x, oy: offset.y };
    setDragging(true);
  };

  const moveDrag = (event: PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    setOffset({
      x: drag.current.ox + event.clientX - drag.current.x,
      y: drag.current.oy + event.clientY - drag.current.y,
    });
  };

  const stopDrag = (event: PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current = null;
    setDragging(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  };

  const modalStyle = { transform: `translate(${offset.x}px, ${offset.y}px)` };
  const dragHandleProps = {
    onPointerDown: startDrag,
    onPointerMove: moveDrag,
    onPointerUp: stopDrag,
    onPointerCancel: stopDrag,
  };

  // A plan approval is special: the controller proposes it when a plan-mode turn
  // ends with a proposal. The plan itself is already shown above as the assistant's
  // reply, so this is just the gate — start coding vs keep planning.
  if (approval.tool === "exit_plan_mode") {
    return (
      <div className="modal-backdrop">
        <div className={`modal modal--plan${dragging ? " modal--dragging" : ""}`} style={modalStyle}>
          <div className="modal__title modal__drag-handle" {...dragHandleProps}>
            {t("approval.planTitle")}
          </div>
          <div className="modal__plannote">{t("approval.planNote")}</div>
          <div className="modal__actions">
            <button className="btn" onClick={() => onAnswer(false, false)}>
              {t("approval.keepPlanning")}
            </button>
            <button className="btn btn--primary" onClick={() => onAnswer(true, false)}>
              {t("approval.proceed")}
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="modal-backdrop">
      <div className={`modal${dragging ? " modal--dragging" : ""}`} style={modalStyle}>
        <div className="modal__title modal__drag-handle" {...dragHandleProps}>
          {t("approval.toolTitle")}
        </div>
        <div className="modal__tool">
          <span className="tool__name">{approval.tool}</span>
        </div>
        {approval.subject && <pre className="modal__subject">{approval.subject}</pre>}
        <div className="modal__actions">
          <button className="btn" onClick={() => onAnswer(false, false)}>
            {t("approval.deny")}
          </button>
          <button className="btn" onClick={() => onAnswer(true, false)}>
            {t("approval.allowOnce")}
          </button>
          <button className="btn btn--primary" onClick={() => onAnswer(true, true)}>
            {t("approval.allowSession")}
          </button>
        </div>
      </div>
    </div>
  );
}
