import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { DeviceSelf } from "../port/share";
import { setViewer } from "../state/viewer";

// How often a device confirms the machine still answers. The event stream
// carries the work; this only keeps the bar's dot honest when it stops.
const CHECK_MS = 10_000;

/** On a paired device, the page says whose machine it is driving. Without it
 *  the page is the window's own, and nothing tells the person holding the
 *  phone that the sessions they are working live somewhere else. The window
 *  and a networked serve's browser get a 404 and draw nothing. */
export function DeviceBar({ hub }: { hub: HubPort }) {
  const [self, setSelf] = useState<DeviceSelf | null>(null);
  const [reachable, setReachable] = useState(true);
  const [arming, setArming] = useState(false);
  const arm = useRef<number | null>(null);

  useEffect(() => {
    let alive = true;
    let timer: number | null = null;
    const read = () =>
      hub.device().then(
        (d) => {
          if (!alive) return false;
          setSelf(d);
          setViewer(d);
          setReachable(true);
          return d !== null;
        },
        () => {
          if (alive) setReachable(false);
          return true;
        },
      );
    // Only a device keeps checking: the window's answer never changes.
    void read().then((device) => {
      if (alive && device) timer = window.setInterval(() => void read(), CHECK_MS);
    });
    return () => {
      alive = false;
      if (timer !== null) window.clearInterval(timer);
      if (arm.current !== null) window.clearTimeout(arm.current);
    };
  }, [hub]);

  if (!self) return null;

  // A second press on the same place, not a dialog: leaving is one tap from
  // being unpaired and has to be meant.
  const leave = () => {
    if (arm.current !== null) window.clearTimeout(arm.current);
    if (!arming) {
      setArming(true);
      arm.current = window.setTimeout(() => setArming(false), 3000);
      return;
    }
    void hub.leaveDevice().finally(() => window.location.reload());
  };

  return (
    <div className="devicebar" data-down={reachable ? undefined : ""} role="status">
      <i aria-hidden="true" />
      <span className="db-what">
        {reachable ? t("正在控制 {machine}", { machine: self.machine }) : t("与 {machine} 的连接已中断", { machine: self.machine })}
      </span>
      <small>{t("本机是 设备 {n}", { n: self.ordinal })}</small>
      <button data-action={arming ? "device.leave" : "device.ask-leave"} data-armed={arming ? "" : undefined} onClick={leave}>
        {arming ? t("确认断开") : t("断开")}
      </button>
    </div>
  );
}
