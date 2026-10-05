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
              ? t("{host} 需要密码", { host: ask.host })
              : t("私钥已被口令锁定")
            : t("首次连接 {host}", { host: ask.host })}
        </h2>

        {secret ? (
          <>
            <p className="h">
              {ask.identityFile
                ? t("解锁 {file}。该口令仅存在于本次连接的内存中，不会写入任何文件。", { file: ask.identityFile })
                : t("仅存在于本次连接的内存中，不会写入任何文件。如需保存，请在设置中填写环境变量名。")}
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
              {t("该机器尚未记录。以下是它出示的指纹 —— 请与从其他渠道获取的指纹核对，一致后再接受。")}
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
            {secret ? t("继续") : t("指纹一致，记住该机器")}
          </button>
        </div>
      </div>
    </div>
  );
}
