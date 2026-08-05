import { useEffect, useId, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import {
  DecisionConfirmBar,
  PromptAction,
  PromptBadge,
  PromptDescriptionDisclosure,
  PromptShelf,
} from "./PromptShelf";

export function ClearContextCard({
  onCancel,
  onConfirm,
}: {
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const t = useT();
  const shelfRef = useRef<HTMLDivElement | null>(null);
  // Default safe choice: cancel.
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [expandedDescriptionId, setExpandedDescriptionId] = useState<string | null>(null);
  const [descriptionTruncated, setDescriptionTruncated] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const instanceId = useId();
  const selectedIndexRef = useRef(0);
  selectedIndexRef.current = selectedIndex;
  const submittingRef = useRef(false);
  submittingRef.current = submitting;
  const onCancelRef = useRef(onCancel);
  onCancelRef.current = onCancel;
  const onConfirmRef = useRef(onConfirm);
  onConfirmRef.current = onConfirm;

  const actions = [
    {
      key: "1",
      label: t("common.cancel"),
      desc: t("clearContext.cancelDesc"),
      tone: "default" as const,
    },
    {
      key: "2",
      label: t("clearContext.clear"),
      desc: t("clearContext.clearDesc"),
      tone: "danger" as const,
    },
  ];
  const selectedDescriptionId = `${instanceId}-description-${selectedIndex}`;
  const descriptionExpanded = expandedDescriptionId === selectedDescriptionId;

  useEffect(() => {
    shelfRef.current?.focus();
    setSelectedIndex(0);
    setSubmitting(false);
  }, []);

  const runSelected = () => {
    if (submittingRef.current) return;
    submittingRef.current = true;
    setSubmitting(true);
    if (selectedIndexRef.current === 1) onConfirmRef.current();
    else onCancelRef.current();
  };

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (submittingRef.current) return;
      const target = event.target instanceof Element ? event.target : null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || (target instanceof HTMLElement && target.isContentEditable)) return;

      if (event.key === "Escape") {
        event.preventDefault();
        submittingRef.current = true;
        setSubmitting(true);
        onCancelRef.current();
        return;
      }
      if (event.key === "ArrowUp" || event.key === "ArrowDown") {
        event.preventDefault();
        setSelectedIndex((i) => (i === 0 ? 1 : 0));
        return;
      }
      if (event.key === "Enter") {
        event.preventDefault();
        runSelected();
        return;
      }
      if (event.key === "1") {
        event.preventDefault();
        setSelectedIndex(0);
        return;
      }
      if (event.key === "2") {
        event.preventDefault();
        setSelectedIndex(1);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  return (
    <PromptShelf
      decision
      className="prompt-shelf--clear-context"
      barRef={shelfRef}
      titleId="clear-context-shelf-title"
      title={t("clearContext.title")}
      badges={<PromptBadge tone="amber">{t("clearContext.badge")}</PromptBadge>}
      meta={t("clearContext.prompt")}
      actions={
        <>
          {actions.map((action, index) => (
            <PromptAction
              key={action.key}
              keyLabel={action.key}
              label={action.label}
              description={action.desc}
              descriptionId={`${instanceId}-description-${index}`}
              descriptionDisclosure
              onDescriptionOverflowChange={selectedIndex === index ? setDescriptionTruncated : undefined}
              onClick={() => {
                if (submitting) return;
                setSelectedIndex(index);
              }}
              selected={selectedIndex === index}
              tone={action.tone}
              disabled={submitting}
            />
          ))}
        </>
      }
      note={
        descriptionTruncated ? (
          <PromptDescriptionDisclosure
            descriptionId={`${selectedDescriptionId}-detail`}
            label={actions[selectedIndex]?.label}
            description={actions[selectedIndex]?.desc ?? ""}
            expanded={descriptionExpanded}
            onToggle={() => setExpandedDescriptionId((current) => current === selectedDescriptionId ? null : selectedDescriptionId)}
            disabled={submitting}
            alwaysVisible={actions[selectedIndex]?.tone === "danger"}
          />
        ) : undefined
      }
      footer={
        <DecisionConfirmBar
          hint={t("decision.selectHint")}
          confirmLabel={t("decision.confirm")}
          onConfirm={runSelected}
          disabled={submitting}
          danger={selectedIndex === 1}
        />
      }
    />
  );
}
