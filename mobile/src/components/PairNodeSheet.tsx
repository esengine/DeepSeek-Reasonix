import { Check, Copy, Link2, QrCode } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import {
  buildPairingUri,
  parsePairingPayload,
  probeNodeHealth,
  type ParsedPairing,
} from "../lib/paired-nodes";
import { t, type Locale } from "../i18n/messages";
import { BottomSheet } from "./BottomSheet";

const DEMO_URL = "http://127.0.0.1:8790";

export function PairNodeSheet({
  open,
  locale,
  onClose,
  onPaired,
}: {
  open: boolean;
  locale: Locale;
  onClose: () => void;
  onPaired: (node: ParsedPairing & { online: boolean }) => void;
}) {
  const [payload, setPayload] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<ParsedPairing | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!open) return;
    setPayload("");
    setError(null);
    setPreview(null);
    setBusy(false);
    setCopied(false);
  }, [open]);

  const demoUri = useMemo(
    () =>
      buildPairingUri({
        baseUrl: DEMO_URL,
        id: "node-local-demo",
        name: "Local Node",
        fingerprint: "demo-fp-local",
      }),
    [],
  );

  const qrValue = preview ? buildPairingUri(preview) : demoUri;

  const tryParse = (raw: string) => {
    setError(null);
    try {
      const p = parsePairingPayload(raw);
      setPreview(p);
      return p;
    } catch (e) {
      setPreview(null);
      setError(e instanceof Error ? e.message : t(locale, "nodes.pairInvalid"));
      return null;
    }
  };

  const pair = async () => {
    const raw = payload.trim() || (preview ? buildPairingUri(preview) : "");
    if (!raw) {
      setError(t(locale, "nodes.pairNeedInput"));
      return;
    }
    const p = tryParse(raw);
    if (!p) return;
    setBusy(true);
    setError(null);
    try {
      const health = await probeNodeHealth(p.baseUrl);
      if (health.nodeId && health.nodeId !== p.id) {
        p.id = health.nodeId;
      }
      onPaired({ ...p, online: health.ok });
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : t(locale, "nodes.pairFailed"));
    } finally {
      setBusy(false);
    }
  };

  const copyDemo = async () => {
    try {
      await navigator.clipboard.writeText(demoUri);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setPayload(demoUri);
      tryParse(demoUri);
    }
  };

  return (
    <BottomSheet
      open={open}
      title={t(locale, "nodes.pair")}
      description={t(locale, "nodes.pairSheetDesc")}
      localeCloseLabel={t(locale, "common.close")}
      onClose={onClose}
      wide
    >
      <div className="pair-body anim-enter">
        <div className="pair-qr-card">
          <div className="pair-qr-frame" data-theme-aware>
            <QRCodeSVG
              value={qrValue}
              size={168}
              level="M"
              bgColor="transparent"
              fgColor="currentColor"
              marginSize={1}
            />
          </div>
          <p className="pair-qr-caption">
            <QrCode size={14} aria-hidden />
            {preview ? t(locale, "nodes.qrPairedPreview") : t(locale, "nodes.qrDemoCaption")}
          </p>
          {preview ? (
            <div className="pair-preview-meta">
              <strong>{preview.name}</strong>
              <span className="mono">{preview.baseUrl}</span>
              {preview.fingerprint ? (
                <span className="mono faint">fp:{preview.fingerprint.slice(0, 16)}</span>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className="sheet-field">
          <label htmlFor="pair-payload">{t(locale, "nodes.pairPaste")}</label>
          <textarea
            id="pair-payload"
            className="pair-textarea"
            rows={3}
            value={payload}
            placeholder={t(locale, "nodes.pairPlaceholder")}
            onChange={(e) => {
              setPayload(e.target.value);
              if (e.target.value.trim()) tryParse(e.target.value);
              else {
                setPreview(null);
                setError(null);
              }
            }}
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
          />
        </div>

        <div className="pair-quick row-gap">
          <button
            type="button"
            className="btn-secondary"
            onClick={() => {
              setPayload(demoUri);
              tryParse(demoUri);
            }}
          >
            <Link2 size={16} />
            {t(locale, "nodes.useDemo")}
          </button>
          <button type="button" className="btn-secondary" onClick={() => void copyDemo()}>
            {copied ? <Check size={16} /> : <Copy size={16} />}
            {copied ? t(locale, "common.done") : t(locale, "nodes.copyDemo")}
          </button>
        </div>

        {error ? <p className="sheet-error">{error}</p> : null}

        <div className="sheet-actions">
          <button
            type="button"
            className="btn-primary"
            disabled={busy || (!payload.trim() && !preview)}
            onClick={() => void pair()}
          >
            {busy ? t(locale, "nodes.pairing") : t(locale, "nodes.pairConfirm")}
          </button>
          <button type="button" className="btn-secondary" onClick={onClose} disabled={busy}>
            {t(locale, "sessions.cancel")}
          </button>
        </div>
      </div>
    </BottomSheet>
  );
}
