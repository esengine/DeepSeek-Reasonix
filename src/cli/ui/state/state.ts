import { getLanguage } from "../../../i18n/index.js";
import type { LanguageCode } from "../../../i18n/types.js";
import type { Card, CardId } from "./cards.js";

export type Mode = "auto" | "ask" | "plan" | "edit";
export type NetworkState = "online" | "slow" | "disconnected" | "reconnecting";
export type ToastTone = "ok" | "info" | "warn" | "err";

export interface SessionInfo {
  readonly id: string;
  readonly branch: string;
  readonly workspace: string;
  readonly model: string;
}

export interface ComposerState {
  value: string;
  cursor: number;
  picker: "slash" | "mention" | "history" | "slasharg" | null;
  shell: boolean;
  abortedHint: boolean;
}

export interface StatusBar {
  mode: Mode;
  network: NetworkState;
  networkDetail?: string;
  cost: number;
  sessionCost: number;
  balance?: number;
  balanceCurrency?: string;
  /** User-togglable cost display currency ("USD" or "CNY"). When set, takes
   *  precedence over `balanceCurrency` for cost formatting. Seeded from
   *  config on mount; toggle by clicking the turn-cost pill in the status bar. */
  costDisplayCurrency?: string;
  cacheHit: number;
  /** Last-turn prompt tokens; drives the context-usage pill. */
  promptTokens?: number;
  /** Model context-window cap (denominator for the usage pill). */
  promptCap?: number;
  /** Cumulative prompt tokens billed across the session — drives the dock "tok ↑" segment. */
  sessionInputTokens: number;
  /** Cumulative completion tokens billed across the session — drives the dock "tok ↓" segment. */
  sessionOutputTokens: number;
  /** Wall-clock ms for the most recent completed turn. */
  lastTurnMs: number;
  countdownSeconds?: number;
  recording?: { sizeBytes: number; events: number; path: string };
  reasoningEffort?: import("../../../config.js").ReasoningEffort;
  /** Bridged-MCP handshake progress. Pill is shown while ready < total. */
  mcpLoading?: { ready: number; total: number };
}

export interface Toast {
  readonly id: string;
  readonly tone: ToastTone;
  readonly title: string;
  readonly detail?: string;
  readonly bornAt: number;
  readonly ttlMs: number;
}

export interface AgentState {
  readonly lang: LanguageCode;
  readonly session: SessionInfo;
  readonly cards: ReadonlyArray<Card>;
  /** id → index in `cards`. Mirrors `cards` exactly; rebuilt on any structural change. */
  readonly cardIndex: ReadonlyMap<CardId, number>;
  /** Smallest cards-index that still needs elision consideration; monotonic per session. */
  readonly elideCursor: number;
  readonly composer: ComposerState;
  readonly status: StatusBar;
  readonly focusedCardId: CardId | null;
  readonly toasts: ReadonlyArray<Toast>;
  readonly turnInProgress: boolean;
}

export function initialState(session: SessionInfo, cards: ReadonlyArray<Card> = []): AgentState {
  const cardIndex = new Map<CardId, number>();
  for (let i = 0; i < cards.length; i++) cardIndex.set(cards[i]!.id, i);
  return {
    lang: getLanguage(),
    session,
    cards,
    cardIndex,
    elideCursor: 0,
    composer: {
      value: "",
      cursor: 0,
      picker: null,
      shell: false,
      abortedHint: false,
    },
    status: {
      mode: "auto",
      network: "online",
      cost: 0,
      sessionCost: 0,
      cacheHit: 0,
      sessionInputTokens: 0,
      sessionOutputTokens: 0,
      lastTurnMs: 0,
    },
    focusedCardId: null,
    toasts: [],
    turnInProgress: false,
  };
}
