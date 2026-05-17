import { Box, Text, useApp } from "ink";
import React, { useEffect, useState } from "react";
import { defaultConfigPath, isPlausibleKey, redactKey, saveApiKey } from "../../config.js";
import { t } from "../../i18n/index.js";
import { MaskedInput } from "./MaskedInput.js";
import { useSetupSceneTrace } from "./hooks/useSceneTrace.js";
import { COLOR, GLYPH, GRADIENT } from "./theme.js";

export interface SetupProps {
  onReady: (apiKey: string) => void;
  /** Integrated rust renderer routes its setup-submit event here — Node's MaskedInput can't read keys when rust owns stdin, so the rust child accumulates the text and Node calls handleSubmit via this ref instead. */
  submitRef?: { current: ((text: string) => void) | null };
}

export function Setup({ onReady, submitRef }: SetupProps) {
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const { exit } = useApp();

  useSetupSceneTrace({ bufferLength: value.length, error: error ?? undefined });

  const handleSubmit = (raw: string) => {
    const trimmed = raw.trim();
    if (trimmed === "/exit" || trimmed === "/quit") {
      exit();
      return;
    }
    if (!isPlausibleKey(trimmed)) {
      setError(t("wizard.apiKeyInvalid"));
      setValue("");
      return;
    }
    try {
      saveApiKey(trimmed);
    } catch (err) {
      setError(t("wizard.reviewSaveError", { message: (err as Error).message }));
      return;
    }
    onReady(trimmed);
  };

  // Re-bind on every render so handleSubmit's captured `onReady` stays fresh.
  // Cleanup is split into a mount-only effect so the ref never goes null between renders.
  useEffect(() => {
    if (submitRef) submitRef.current = handleSubmit;
  });
  useEffect(() => {
    if (!submitRef) return undefined;
    return () => {
      submitRef.current = null;
    };
  }, [submitRef]);

  return (
    <Box flexDirection="column" paddingX={1} marginY={1}>
      <Box>
        <Text bold color={GRADIENT[0]}>
          {GLYPH.brand}
        </Text>
        <Text>{"  "}</Text>
        <Text bold>{t("wizard.welcomeTitle")}</Text>
      </Box>
      <Box marginTop={1}>
        <Text color={COLOR.info}>{t("wizard.apiKeyPrompt")}</Text>
      </Box>
      <Box>
        <Text dimColor>{`  ${t("wizard.apiKeyGetOne")}`}</Text>
      </Box>
      <Box>
        <Text dimColor>{t("wizard.apiKeySavedLocally", { path: defaultConfigPath() })}</Text>
      </Box>
      <Box marginTop={1}>
        <Text bold color={COLOR.brand}>
          {GLYPH.bar}
        </Text>
        <Text bold color={COLOR.primary}>
          {" › "}
        </Text>
        <MaskedInput
          value={value}
          onChange={setValue}
          onSubmit={handleSubmit}
          mask="•"
          placeholder={t("wizard.apiKeyPlaceholder")}
        />
      </Box>
      {error ? (
        <Box marginTop={1}>
          <Text color={COLOR.err} bold>
            {GLYPH.err}
          </Text>
          <Text color={COLOR.err}>{`  ${error}`}</Text>
        </Box>
      ) : value ? (
        <Box marginTop={1}>
          <Text dimColor>{t("wizard.apiKeyPreview", { redacted: redactKey(value) })}</Text>
        </Box>
      ) : null}
      <Box marginTop={1}>
        <Text dimColor>{t("wizard.exitHint")}</Text>
      </Box>
    </Box>
  );
}
