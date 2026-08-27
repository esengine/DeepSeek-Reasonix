import { TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX } from "./transcriptScrollGeometry";

/**
 * WebKit can rubber-band the final downward wheel backwards when less than
 * one wheel delta remains. Claim only that physical tail destination; the
 * arbiter remains the owner of the actual write.
 */
export function shouldClaimTranscriptTailFromWheel(
  bottomDistance: number,
  deltaY: number,
  layoutTransient: boolean,
): boolean {
  return !layoutTransient
    && deltaY > 0
    && bottomDistance > TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX
    && bottomDistance <= Math.max(TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX, deltaY + TRANSCRIPT_AT_BOTTOM_THRESHOLD_PX);
}
