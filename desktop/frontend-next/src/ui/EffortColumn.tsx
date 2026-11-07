import { EffortSlider } from "./EffortSlider";
import { MenuRow, type MenuItem } from "./Menu";
import { useFolded } from "./usefold";

const isLevel = (it: MenuItem) => !it.header && it.toggle === undefined && it.strength !== undefined;

export function EffortColumn({ items, current, onPick }: { items: MenuItem[]; current: string; onPick: (value: string) => void }) {
  const phone = useFolded("phone");
  const levels = items.filter(isLevel);
  const heading = items.find((it) => it.header);
  // A phone has no room for a row of words per level: they become one slider.
  const slide = phone && levels.length >= 2;
  return (
    <>
      {items.map((it) => {
        if (it.header) {
          return (
            <div key={it.value} className="mi head">
              <span className="lb">{it.label}</span>
              {it.right && <span className="rt">{it.right}</span>}
            </div>
          );
        }
        if (slide && isLevel(it)) {
          return it === levels[0]
            ? <EffortSlider key="slider" name={heading?.label ?? ""} model={heading?.right} levels={levels} current={current} onPick={onPick} />
            : null;
        }
        return <MenuRow key={it.value} it={it} id={{ "data-action": "reasoning.effort" }} current={current} onTake={onPick} />;
      })}
    </>
  );
}
