import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Button, Card, Group as MantineGroup, Stack, Text, Title } from "@mantine/core";
import { useSignalValue } from "@a2ui/react/v0_9";
import type { SurfaceActionPort, SurfaceActionRecord } from "../../../port/surface";
import type { SurfaceSpec } from "../spec";
import { SurfaceKit } from "../SurfaceKit";
import { SurfaceView, useSurfaceRuntime } from "./view";
import type { SurfaceRuntime } from "./runtime";
import { t } from "../../../i18n";
import { reason } from "../../../i18n/kernel";

export default function SurfaceActionCard({spec,callId,port}:{spec:SurfaceSpec;callId:string;port?:SurfaceActionPort}) {
  const [state,setState] = useState<SurfaceActionRecord>();
  const [error,setError] = useState("");
  const generation = useRef(0);
  const refresh = useCallback(async () => {
    const id = ++generation.current;
    if (!port) {setError(t("chat.this_surface_has_expired_wait"));return;}
    try {const next=await port.surfaceActionState(callId);if(id===generation.current){setState(next);setError("");return next;}}
    catch(e){if(id===generation.current)setError(reason(e));}
  },[callId,port]);
  useEffect(()=>{void refresh();return()=>{generation.current++;};},[refresh]);
  return <SurfaceKit>{state ? <ActionSurface spec={spec} state={state} setState={setState} port={port} loadError={error} refresh={refresh}/>
    : <Card withBorder padding="lg">{spec.title && <Title order={3} size="h4">{spec.title}</Title>}{error && <Stack><Alert color="red" role="alert">{error}</Alert><Button data-action="surface.refresh" onClick={()=>void refresh()}>{t("common.retry")}</Button></Stack>}</Card>}</SurfaceKit>;
}

function ActionSurface(props:{spec:SurfaceSpec;state:SurfaceActionRecord;setState:(state:SurfaceActionRecord)=>void;port?:SurfaceActionPort;loadError:string;refresh:()=>Promise<SurfaceActionRecord|undefined>}) {
  const restored = props.state.status ? props.state.action.values : undefined;
  const runtime=useSurfaceRuntime(props.spec.body,restored);
  return runtime ? <ActionForm {...props} runtime={runtime}/> : null;
}

function ActionForm({spec,runtime,state,setState,port,loadError,refresh}:{spec:SurfaceSpec;runtime:SurfaceRuntime;state:SurfaceActionRecord;setState:(state:SurfaceActionRecord)=>void;port?:SurfaceActionPort;loadError:string;refresh:()=>Promise<SurfaceActionRecord|undefined>}) {
  useSignalValue(runtime.surface.dataModel.getSignal("/fields"));
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState("");
  const [delivery,setDelivery]=useState<"queue"|"steer">("queue");
  const request=useRef<{body:string;id:string}>(undefined);
  const sealed=state?.status==="accepted" || !state?.active;
  const {values,valid}=runtime.answers();
  const send=async(actionId:string)=>{
    if(!state || !port || busy || sealed || !valid)return;
    const body=JSON.stringify([actionId,delivery,values]);
    if(request.current?.body!==body)request.current={body,id:crypto.randomUUID()};
    const action={...state.action,actionId,values,requestId:request.current.id};
    setBusy(true);setError("");
    try{const rec=await port.surfaceAction({action,delivery});setState({...state,action,itemId:rec.itemId,status:"accepted",canWithdraw:true});void refresh();}
    catch(e){const recovered=await refresh();setError(recovered?.status==="accepted" && recovered.action.requestId===action.requestId ? "" : reason(e));}
    finally{setBusy(false);}
  };
  const withdraw=async()=>{
    if(!port || !state?.itemId || busy)return;
    setBusy(true);setError("");
    try{await port.cancelQueued(state.itemId);setState({...state,status:"withdrawn"});request.current=undefined;void refresh();}
    catch(e){setError(reason(e));}
    finally{setBusy(false);}
  };
  return <Card withBorder padding="lg" radius="md" className="native-surface" data-surface="" aria-busy={busy}>
    <Stack gap="lg">
      {spec.title && <Title order={3} size="h4">{spec.title}</Title>}
      <SurfaceView runtime={runtime} inputs disabled={!state||busy||sealed} actionDisabled={!valid} onAction={id=>void send(id)}/>
      {(error||loadError) && <Alert color="red" role="alert">{error||loadError}</Alert>}
      {sealed ? <MantineGroup justify="space-between"><Text size="sm" c="dimmed">{state.status==="withdrawn"?t("chat.withdrawn"):t("chat.submitted")}</Text>{state.canWithdraw && <Button variant="subtle" data-action="queue.cancel" data-target={state.itemId} disabled={busy} onClick={()=>void withdraw()}>{t("chat.take_it_back")}</Button>}</MantineGroup>
      : <MantineGroup><Button variant={delivery==="queue"?"light":"subtle"} data-action="surface.delivery" data-value="queue" disabled={busy} onClick={()=>setDelivery("queue")}>{t("common.queued")}</Button><Button variant={delivery==="steer"?"light":"subtle"} data-action="surface.delivery" data-value="steer" disabled={busy} onClick={()=>setDelivery("steer")}>{t("chat.steer_current_task")}</Button></MantineGroup>}
    </Stack>
  </Card>;
}
