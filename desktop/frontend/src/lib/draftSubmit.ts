import type { AppBindings } from "./bridge";
import { draftIDsFromSubmit } from "./draftCredentials";

export function draftSubmit(app: AppBindings, tabId: string, submit: string, submissionId: string) {
  const draftIDs = draftIDsFromSubmit(submit);
  if (draftIDs.length > 0 && typeof app.StartTurnForTabWithDrafts === "function") {
    return app.StartTurnForTabWithDrafts(tabId, submit, submissionId, draftIDs);
  }
  if (typeof app.StartTurnForTab === "function") return app.StartTurnForTab(tabId, submit, submissionId);
  return app.SubmitToTabWithID(tabId, submit, submissionId);
}
