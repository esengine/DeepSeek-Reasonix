import { useState } from "react";
import { Badge, Button, Group as MantineGroup, Paper, Stack, Text, TextInput, Title } from "@mantine/core";
import { Workspace, type RichInputProps } from "./Workspace";
import { OptionImage } from "../media";
import { t } from "../../../i18n";

export default function Comparison({ node, value, disabled, change }: RichInputProps) {
  const options = node.field!.options!;
  const selected = Array.isArray(value) ? value as string[] : typeof value === "string" ? [value] : [];
  const [search, setSearch] = useState("");
  const [group, setGroup] = useState("");
  const [shortlist, setShortlist] = useState(false);
  const [table, setTable] = useState(false);
  const [differences, setDifferences] = useState(false);
  const [ascending, setAscending] = useState(false);
  const groups = [...new Set(options.map(o => o.group).filter((g): g is string => !!g))];
  const comparablePrices = new Set(options.filter(o => o.price !== undefined).map(o => o.currency)).size === 1;
  const visible = options.filter(o => (!group || o.group === group) && (!shortlist || selected.includes(o.value)) && `${o.label} ${o.description ?? ""}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
  if (ascending) visible.sort((a, b) => (a.price ?? Infinity) - (b.price ?? Infinity));
  const facts = [...new Set(visible.flatMap(o => o.facts?.map(f => f.label) ?? []))];
  const priceDiffers = new Set(visible.map(o => JSON.stringify([o.currency, o.price]))).size > 1;
  const differing = (f: string) => new Set(visible.map(o => o.facts?.find(v => v.label === f)?.value)).size > 1;
  const select = (id: string) => { if (!disabled) change(node.field!.type === "choice" ? id : selected.includes(id) ? selected.filter(v => v !== id) : [...selected, id]); };
  return <Workspace title={node.label} badge={t("chat.compare_choose")} subtitle={t("chat.filter_options_compare_the_details")}>
    <MantineGroup justify="space-between"><TextInput style={{ flex: "1 1 200px" }} aria-label={t("chat.search_options")} placeholder={t("chat.search_options")} value={search} data-action="surface.explore" onChange={e => setSearch(e.target.value)} /><MantineGroup gap="xs">
      <Button size="xs" variant={shortlist ? "light" : "subtle"} data-action="surface.explore" onClick={() => setShortlist(v => !v)}>{t("chat.selected_only")}</Button>
      <Button size="xs" variant={table ? "light" : "subtle"} data-action="surface.explore" onClick={() => setTable(v => !v)}>{table ? t("chat.card_view") : t("chat.compare_details")}</Button>
      {table && <Button size="xs" variant={differences ? "light" : "subtle"} aria-pressed={differences} data-action="surface.explore" onClick={() => setDifferences(v => !v)}>{t("chat.differences_only")}</Button>}
      {comparablePrices && <Button size="xs" variant={ascending ? "light" : "subtle"} data-action="surface.explore" onClick={() => setAscending(v => !v)}>{t("chat.sort_by_price")}</Button>}
    </MantineGroup></MantineGroup>
    <MantineGroup gap="xs"><Button size="xs" variant={!group ? "light" : "subtle"} data-action="surface.explore" onClick={() => setGroup("")}>{t("chat.all_options")}</Button>{groups.map(g => <Button key={g} size="xs" variant={group === g ? "light" : "subtle"} data-action="surface.explore" onClick={() => setGroup(g)}>{g}</Button>)}<Text c="dimmed" size="xs">{t("chat.n_selected", { n: selected.length })}</Text></MantineGroup>
    {!visible.length && <Text role="status" c="dimmed">{t("chat.no_options_match_these_filters")}</Text>}
    {table && visible.length > 0 ? <div className="rich-table"><table aria-label={node.label}><thead><tr><th>{t("chat.attribute")}</th>{visible.map(o => <th key={o.value}>{o.label}</th>)}</tr></thead><tbody>
      {(!differences || priceDiffers) && <tr data-different={priceDiffers}><th>{t("chat.price")}</th>{visible.map(o => <td key={o.value}>{o.price === undefined ? "—" : `${o.currency} ${o.price.toLocaleString()}`}</td>)}</tr>}
      {facts.filter(f => !differences || differing(f)).map(f => <tr key={f} data-different={differing(f)}><th>{f}</th>{visible.map(o => <td key={o.value}>{o.facts?.find(v => v.label === f)?.value ?? "—"}</td>)}</tr>)}
      <tr><th>{t("chat.select")}</th>{visible.map(o => <td key={o.value}><Button size="xs" variant={selected.includes(o.value) ? "light" : "filled"} disabled={disabled} aria-pressed={selected.includes(o.value)} aria-label={t("chat.select_name", { name: o.label })} data-action="surface.edit" data-target={node.key} onClick={() => select(o.value)}>{selected.includes(o.value) ? t("chat.selected") : t("chat.select")}</Button></td>)}</tr>
    </tbody></table></div> : <div className="rich-products">{visible.map(o => <Paper className="rich-product" data-selected={selected.includes(o.value)} radius="lg" p="lg" key={o.value}>
      <Stack h="100%" gap="md"><OptionImage image={o.image} fit="contain" /><MantineGroup justify="space-between">{o.group && <Badge color="gray" variant="light">{o.group}</Badge>}{selected.includes(o.value) && <Badge color="teal">{t("chat.selected")}</Badge>}</MantineGroup><Title order={4} size="xl">{o.label}</Title>
        {o.price !== undefined && <Text className="rich-product-price" fw={600}><Text component="span" size="xs" c="dimmed">{o.currency}</Text> {o.price.toLocaleString()}</Text>}
        {o.description && <Text c="dimmed" size="sm">{o.description}</Text>}
        <Stack gap="xs" style={{ flex: 1 }}>{o.facts?.map(f => <MantineGroup key={f.label} justify="space-between" align="flex-start" wrap="nowrap"><Text c="dimmed" size="sm">{f.label}</Text><Text size="sm" ta="right">{f.value}</Text></MantineGroup>)}</Stack>
        {o.preview && <Text size="sm">{o.preview}</Text>}
        <Button color="teal" variant={selected.includes(o.value) ? "light" : "filled"} disabled={disabled} aria-pressed={selected.includes(o.value)} aria-label={t("chat.select_name", { name: o.label })} data-action="surface.edit" data-target={node.key} onClick={() => select(o.value)}>{selected.includes(o.value) ? t("chat.selected") : t("chat.select")}</Button>
      </Stack>
    </Paper>)}</div>}
  </Workspace>;
}
