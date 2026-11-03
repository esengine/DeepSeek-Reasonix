import { MenuRow, type MenuItem } from "./Menu";

export function EffortColumn({ items, current, onPick }: { items: MenuItem[]; current: string; onPick: (value: string) => void }) {
  return (
    <>
      {items.map((it) =>
        it.header ? (
          <div key={it.value} className="mi head">
            <span className="lb">{it.label}</span>
            {it.right && <span className="rt">{it.right}</span>}
          </div>
        ) : (
          <MenuRow key={it.value} it={it} id={{ "data-action": "reasoning.effort" }} current={current} onTake={onPick} />
        ),
      )}
    </>
  );
}
