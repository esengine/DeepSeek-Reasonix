export type TranscriptScrollMode =
  | "tail-follow"
  | "manual"
  | "native-selecting"
  | "logical-selecting"
  | "programmatic";

export type TranscriptScrollOwner =
  | "jump"
  | "rewind"
  | "jump-bottom"
  | "custom-scrollbar"
  | "selection-edge-scroll";

export function isTranscriptSelectionMode(mode: TranscriptScrollMode): boolean {
  return mode === "native-selecting" || mode === "logical-selecting";
}
