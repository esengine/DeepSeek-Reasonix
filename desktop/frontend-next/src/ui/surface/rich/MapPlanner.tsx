import { StudioIcon } from "../../StudioIcon";
import { useLayoutEffect, useRef, useState } from "react";
import { Badge, Button, Group as MantineGroup, Paper, Stack, Text } from "@mantine/core";
import { Workspace, type RichInputProps } from "./Workspace";
import { lazySurface } from "../a2ui/lazy";
import { OptionImage } from "../media";
import { t } from "../../../i18n";

const MapCanvas = lazySurface(() => import("./MapCanvas"));
export default function MapPlanner({ node, value, disabled, change }: RichInputProps) {
  const places = node.field!.options!;
  const order = Array.isArray(value) ? value as string[] : [];
  const [active, setActive] = useState(places[0]?.value);
  const [focusVersion, setFocusVersion] = useState(0);
  const focus = (id: string) => { setActive(id); setFocusVersion(v => v + 1); };
  const listRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (!focusVersion || !listRef.current || !activeRef.current) return;
    const list = listRef.current.getBoundingClientRect(), row = activeRef.current.getBoundingClientRect();
    const delta = row.top < list.top ? row.top - list.top : row.bottom > list.bottom ? row.bottom - list.bottom : 0;
    listRef.current.scrollTop += delta;
  }, [active, focusVersion]);
  const [group, setGroup] = useState("");
  const groups = [...new Set(places.map(p => p.group).filter((v): v is string => !!v))];
  const visible = [...order.flatMap(id => places.filter(p => p.value === id)), ...places.filter(p => !order.includes(p.value))].filter(p => !group || p.group === group);
  const move = (id: string, delta: number) => { const next = [...order]; const from = next.indexOf(id), to = from + delta; if (from < 0 || to < 0 || to >= next.length || disabled) return; [next[from], next[to]] = [next[to], next[from]]; change(next); };
  return <Workspace title={node.label} badge={t("chat.map_itinerary")} subtitle={t("chat.choose_places_and_arrange_the")}>
    <MantineGroup gap="xs"><Button size="xs" variant={!group ? "light" : "subtle"} data-action="surface.explore" onClick={() => setGroup("")}>{t("chat.all_places")}</Button>{groups.map(g => <Button key={g} size="xs" variant={group === g ? "light" : "subtle"} data-action="surface.explore" onClick={() => setGroup(g)}>{g}</Button>)}<Badge variant="light" color="gray">{t("chat.n_selected", { n: order.length })}</Badge></MantineGroup>
    <div className="rich-split">
      <Stack gap="xs"><MapCanvas places={visible} order={order} active={active} focusVersion={focusVersion} focus={focus} /><Text size="xs" c="dimmed">{t("chat.dashed_lines_show_visit_order")}</Text></Stack>
      <Stack ref={listRef} gap="sm" className="rich-map-list">
        {visible.map(p => { const at = order.indexOf(p.value), included = at >= 0; return <Paper ref={active === p.value ? activeRef : undefined} key={p.value} p="md" radius="md" className="rich-stop" data-active={active === p.value}>
          <Stack gap="sm"><MantineGroup justify="space-between" wrap="nowrap"><Badge circle color={included ? "teal" : "gray"} variant="light">{included ? at + 1 : <StudioIcon name="plus" />}</Badge><Button variant="subtle" size="sm" data-action="surface.explore" onClick={() => focus(p.value)}>{p.label}</Button>{p.group && <Text size="xs" c="dimmed">{p.group}</Text>}</MantineGroup>
          {active === p.value && <OptionImage image={p.image} />}
          {p.description && <Text size="sm">{p.description}</Text>}
          {active === p.value && p.facts?.map(f => <MantineGroup key={f.label} justify="space-between"><Text c="dimmed" size="xs">{f.label}</Text><Text size="xs">{f.value}</Text></MantineGroup>)}
          <MantineGroup gap="xs"><Button variant={included ? "light" : "filled"} color="teal" size="xs" disabled={disabled} data-action="surface.edit" data-target={node.key} onClick={() => change(included ? order.filter(id => id !== p.value) : [...order, p.value])}>{included ? t("chat.remove_stop") : t("chat.add_stop")}</Button>
            {included && <><Button size="xs" variant="subtle" aria-label={t("common.move_name_up", { name: p.label })} disabled={disabled || at === 0} data-action="surface.edit" data-target={node.key} onClick={() => move(p.value, -1)}><StudioIcon name="arrow" /></Button><Button size="xs" variant="subtle" aria-label={t("common.move_name_down", { name: p.label })} disabled={disabled || at === order.length - 1} data-action="surface.edit" data-target={node.key} onClick={() => move(p.value, 1)}><StudioIcon name="arrowDown" /></Button></>}
          </MantineGroup></Stack>
        </Paper>; })}
      </Stack>
    </div>
  </Workspace>;
}
