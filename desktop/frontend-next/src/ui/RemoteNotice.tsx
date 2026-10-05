import { useEffect, useState } from "react";
import { t } from "../i18n";

interface Props {
  title: string;
  body: string;
  action: string;
  onAction: () => void;
  waitS?: number;
  retryWhenOnline?: boolean;
}

export function RemoteNotice({ title, body, action, onAction, waitS = 0, retryWhenOnline = false }: Props) {
  const [left, setLeft] = useState(waitS);
  useEffect(() => {
    setLeft(waitS);
    if (waitS <= 0) return;
    const timer = setInterval(() => setLeft((n) => (n <= 1 ? (clearInterval(timer), 0) : n - 1)), 1000);
    return () => clearInterval(timer);
  }, [waitS]);
  useEffect(() => {
    if (!retryWhenOnline) return;
    addEventListener("online", onAction);
    return () => removeEventListener("online", onAction);
  }, [retryWhenOnline, onAction]);
  return (
    <div className="remote-closed" role="alert" aria-label={title}>
      <span aria-hidden="true" />
      <div>
        <b>{title}</b>
        <p>{body}</p>
      </div>
      <button disabled={left > 0} onClick={onAction}>
        {left > 0 ? t("{action}（{n}）", { action, n: left }) : action}
      </button>
    </div>
  );
}
