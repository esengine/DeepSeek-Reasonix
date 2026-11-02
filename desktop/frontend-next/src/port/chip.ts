/** A skill chip as the kernel takes it; offset only orders chips in a line. */
export interface InvocationRequest {
  name: string;
  kind: "skill" | "subagent";
  offset: number;
}

/** A line holding chips: submit is what the model reads, chips taken out. */
export interface ChipCall {
  submit: string;
  invocations: InvocationRequest[];
  // What the transcript shows for this line: the typed text with the chips'
  // host tokens taken out. Drawn only — the kernel projects the same text from
  // the line it stored, so the two cannot disagree after a reload.
  display?: string;
}
