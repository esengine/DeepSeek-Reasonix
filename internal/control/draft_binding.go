package control

import (
	"context"

	"reasonix/internal/attachment"
)

func (c *Controller) RebindDraftImage(ctx context.Context, id string) (attachment.DraftCredential, error) {
	svc := c.attachmentService()
	draft, ok := svc.Drafts().Lookup(c.attachmentScope(), id)
	if !ok {
		return attachment.DraftCredential{}, attachment.Error{Code: attachment.CodeMissing, Message: "draft credential is not valid", Retry: true}
	}
	if _, err := svc.PrepareBatch(ctx, []attachment.Source{{Existing: &draft.Ref, DisplayName: draft.DisplayName}}); err != nil {
		return attachment.DraftCredential{}, err
	}
	return svc.Drafts().RebindVerified(c.attachmentScope(), id)
}

func (c *Controller) rebindPreparedDrafts(req SubmissionRequest, legacy []string) error {
	ids := append([]string(nil), legacy...)
	for _, item := range req.Attachments {
		if item.DraftID != "" {
			ids = append(ids, item.DraftID)
		}
	}
	for _, id := range ids {
		// Queue edits may carry an already admitted, authorized reference.
		if req.frozenSources["draft:"+id] != nil {
			continue
		}
		if _, err := c.attachmentService().Drafts().RebindVerified(c.attachmentScope(), id); err != nil {
			return err
		}
	}
	return nil
}
