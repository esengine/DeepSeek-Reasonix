import { lazy, Suspense } from "react";
import type { ChartSpec } from "./spec";

function loader() {
  return lazy(async () => {
    try {
      return { default: (await import("./ChartCard")).ChartCard };
    } catch (err) {
      Chart = loader();
      throw err;
    }
  });
}

let Chart = loader();

export function LazyChart({ spec, callId }: { spec: ChartSpec; callId?: string }) {
  return (
    <Suspense fallback={<div className="chart-hold" />}>
      <Chart spec={spec} callId={callId} />
    </Suspense>
  );
}
