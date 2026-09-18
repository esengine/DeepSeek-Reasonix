export interface WireImageIdentity {
  messageId: string;
  imageOrdinal: number;
  contentDigest: string;
}

export interface WireImageRecoveryAction {
  id: string;
  reason: string;
  candidates: Array<{ identity: WireImageIdentity; label: string }>;
}
