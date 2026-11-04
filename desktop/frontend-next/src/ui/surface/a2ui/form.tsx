import { Alert, Button, Card, Group as MantineGroup, Stack, Text, Title } from "@mantine/core";
import { SurfaceKit } from "../SurfaceKit";
import { useState } from "react";
import { useSignalValue } from "@a2ui/react/v0_9";
import type { SurfaceCardProps } from "../SurfaceCard";
import type { SurfaceSpec } from "../spec";
import { SurfaceView, useSurfaceRuntime } from "./view";
import type { SurfaceRuntime } from "./runtime";
import { t } from "../../../i18n";
import { reason } from "../../../i18n/kernel";

const emptyValues = Object.freeze({});
type Props = SurfaceCardProps & { spec: SurfaceSpec };

export default function NativeSurface({ item, onAnswer, spec }: Props) {
  const runtime = useSurfaceRuntime(spec.body, item.answered !== undefined ? item.surfaceValues ?? emptyValues : undefined);
  return runtime ? <SurfaceKit><NativeForm item={item} onAnswer={onAnswer} spec={spec} runtime={runtime} /></SurfaceKit> : null;
}

function NativeForm({ item, onAnswer, spec, runtime }: Props & { runtime: SurfaceRuntime }) {
  useSignalValue(runtime.surface.dataModel.getSignal("/fields"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const sealed = item.answered !== undefined;
  const { values, valid } = runtime.answers();
  const send = async (dismiss = false) => {
    if (busy || sealed) return;
    setBusy(true); setError("");
    try { await onAnswer(item.id, item.ask.id, dismiss ? [] : [{ questionId: item.ask.questions[0].id, selected: [], value: values }]); }
    catch (e) { setError(reason(e)); }
    finally { setBusy(false); }
  };
  return <Card withBorder padding="lg" radius="md" className="native-surface" data-surface="" data-sealed={sealed ? "" : undefined} aria-busy={busy}>
    <Stack gap="lg">
    {spec.title && <Title order={3} size="h4">{spec.title}</Title>}
    {!item.answeredElsewhere && <SurfaceView runtime={runtime} disabled={sealed || busy} inputs />}
    {error && <Alert color="red" role="alert">{error}</Alert>}
    {sealed ? <Text size="sm" c="dimmed" fw={500}>{item.answeredElsewhere ? t("chat.handled_in_another_window_refer") : item.surfaceValues ? t("chat.submitted") : t("common.no_selection")}</Text> : <MantineGroup justify="flex-end">
      <Button variant="subtle" type="button" data-action="ask.answer" data-value="none" disabled={busy} onClick={() => void send(true)}>{t("chat.skip_for_now")}</Button>
      <Button type="button" data-action="ask.answer" data-value="chosen" disabled={!valid || busy} onClick={() => void send()}>{busy ? t("common.submitting") : t("chat.submit")}</Button>
    </MantineGroup>}
    </Stack>
  </Card>;
}
