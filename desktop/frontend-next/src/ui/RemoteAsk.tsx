import { useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { RemoteAsk as Ask } from "../port/remote";
import { listenAction } from "./listen";

interface Props {
  ask: Ask;
  onAnswer: (id: string, ok: boolean, text: string) => void;
}

// A connect is stopped on this. Two shapes: a fingerprint to compare, or a
// secret to type — and the first is the one that must not be made easy.
export function RemoteAsk({ ask, onAnswer }: Props) {
  const [text, setText] = useState("");
  const box = useRef<HTMLInputElement>(null);
  const secret = ask.kind !== "hostkey";

  useEffect(() => {
    setText("");
    box.current?.focus();
  }, [ask.askId]);

  // Escape declines. It has to reach the link either way: a dialog dismissed
  // with no answer would leave the dial waiting for one until it times out.
  // The same answer the button gives, so it says so: two entry points, one
  // action, and nothing infers that from them both calling onAnswer.
  useEffect(() => {
    const onKey = (e: Event) => {
      if ((e as KeyboardEvent).key === "Escape") onAnswer(ask.askId, false, "");
    };
    return listenAction(window, "keydown", { action: "remote-ask.answer", value: "decline", listener: onKey });
  }, [ask.askId, onAnswer]);

  return (
    <div className="askveil" role="dialog" aria-modal="true" aria-labelledby="ask-t">
      <div className="askcard">
        <h2 id="ask-t">
          {secret
            ? ask.kind === "password"
              ? t("{host} 要密码", { host: ask.host })
              : t("私钥被口令锁着")
            : t("第一次连 {host}", { host: ask.host })}
        </h2>

        {secret ? (
          <>
            <p className="h">
              {ask.identityFile
                ? t("解开 {file}。它只存在这次连接的内存里，不会写进任何文件。", { file: ask.identityFile })
                : t("只存在这次连接的内存里，不会写进任何文件。想让它记住，在设置里填一个环境变量名。")}
            </p>
            <input
              ref={box}
              data-action-keydown="remote-ask.answer"
              type="password"
              value={text}
              autoFocus
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") onAnswer(ask.askId, true, text);
              }}
            />
          </>
        ) : (
          <>
            <p className="h">
              {t("这台机器还没见过。下面是它出示的指纹 —— 跟你从别处拿到的那份对一下，一致才接受。")}
            </p>
            <dl className="askfacts">
              <dt>{t("地址")}</dt>
              <dd>{ask.address || ask.host}</dd>
              <dt>{t("算法")}</dt>
              <dd>{ask.keyType}</dd>
              <dt>{t("指纹")}</dt>
              {/* Wrapped, not truncated: a fingerprint with its middle cut out
                  is a fingerprint nobody can compare. */}
              <dd className="fp">{ask.fingerprint}</dd>
            </dl>
          </>
        )}

        <div className="askact">
          {/* The way out takes the focus on a fingerprint: Enter is a reflex,
              and accepting a key nobody read is the one thing this prevents. */}
          <button
            data-action="remote-ask.answer"
            data-value="decline"
            autoFocus={!secret}
            onClick={() => onAnswer(ask.askId, false, "")}
          >
            {t("取消")}
          </button>
          <button data-action="remote-ask.answer" data-value="accept" data-go="" onClick={() => onAnswer(ask.askId, true, text)}>
            {secret ? t("继续") : t("对得上，记住它")}
          </button>
        </div>
      </div>
    </div>
  );
}
