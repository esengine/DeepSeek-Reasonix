package control

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"

	"reasonix/internal/imageinput"
	"reasonix/internal/session"
)

// PreparedSubmission is a host-only immutable admission candidate. Its fields
// cannot be supplied over RPC; source paths are consumed only during preparation.
type PreparedSubmission struct {
	owner   *Controller
	scope   string
	request SubmissionRequest
	images  preparedImageReferences
}

func (p *PreparedSubmission) HasImages() bool { return p != nil && len(p.images.inputs) > 0 }

func (c *Controller) PrepareSubmission(ctx context.Context, req SubmissionRequest) (*PreparedSubmission, error) {
	ctx, cancel := c.NewAttachmentOperationContext(ctx)
	defer cancel()
	if len(req.ID) > 256 || strings.ContainsAny(req.ID, "\x00\r\n") {
		return nil, errors.New("invalid submission identity")
	}
	req = cloneSubmissionRequest(req)
	p := &PreparedSubmission{owner: c, scope: c.attachmentScope(), request: req}
	if _, found, err := c.LookupSubmissionContext(ctx, req); found || err != nil {
		return p, err
	}
	images, failures := c.prepareSubmissionImagesContext(ctx, req)
	if len(failures) > 0 {
		// A concurrent retry may have completed while this request read a now
		// released draft. A durable receipt takes precedence over source access.
		if _, found, err := c.LookupSubmissionContext(ctx, req); found || err != nil {
			return p, err
		}
		return nil, ImageReferenceFailures(failures)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(images.inputs) > 0 && c.selection.ref != "" && !c.imageInputEnabled() {
		svc := imageinput.New(imageinput.Config{Model: c.visionModel, Resolve: c.visionProviderResolver, Select: c.visionModelSelector})
		if c.executor != nil && c.executor.ImageInput() != nil {
			svc = c.executor.ImageInput()
		}
		if _, err := svc.SelectModel(c.selection.ref, nil); err != nil {
			return nil, err
		}
	}
	p.images = images
	return p, nil
}

func cloneSubmissionRequest(req SubmissionRequest) SubmissionRequest {
	req.Invocations = slices.Clone(req.Invocations)
	req.DraftIDs = slices.Clone(req.DraftIDs)
	req.AttachmentDigests = slices.Clone(req.AttachmentDigests)
	req.Attachments = slices.Clone(req.Attachments)
	for i := range req.Attachments {
		if ref := req.Attachments[i].Reference; ref != nil {
			copy := *ref
			req.Attachments[i].Reference = &copy
		}
	}
	req.frozenSources = maps.Clone(req.frozenSources)
	req.inheritedSources = slices.Clone(req.inheritedSources)
	return req
}

func (c *Controller) SubmitPreparedWithSetup(ctx context.Context, prepared *PreparedSubmission, setup func() error) (session.SubmissionReceipt, error) {
	if prepared == nil || prepared.owner != c {
		return session.SubmissionReceipt{}, errors.New("invalid prepared submission owner")
	}
	return c.acceptPreparedSubmission(ctx, prepared, setup, func(admission turnAdmission) {
		c.submitIdentifiedRequestLocked(prepared.request, admission)
	})
}
