package control

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"reasonix/internal/attachment"
	"reasonix/internal/config"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

func (c *Controller) StageImage(ctx context.Context, displayName, mime, dataURL string) (attachment.DraftCredential, error) {
	ctx, cancel := c.NewAttachmentOperationContext(ctx)
	defer cancel()
	scope := c.attachmentScope()
	svc := c.attachmentService()
	prepared, err := svc.PrepareBatch(ctx, []attachment.Source{{DisplayName: displayName, DeclaredMIME: mime, DataURL: dataURL}})
	if err != nil {
		return attachment.DraftCredential{}, err
	}
	refs, err := svc.CommitBatch(ctx, prepared)
	if err != nil {
		return attachment.DraftCredential{}, err
	}
	if err := ctx.Err(); err != nil {
		return attachment.DraftCredential{}, err
	}
	if scope != c.attachmentScope() {
		return attachment.DraftCredential{}, attachment.Error{Code: attachment.CodeChanged, Message: "attachment owner changed; please retry"}
	}
	return svc.Drafts().Issue(scope, refs[0]), nil
}

func (c *Controller) ReadDraftImage(ctx context.Context, draftID string) (attachment.DraftCredential, []byte, error) {
	draft, ok := c.attachmentService().Drafts().Lookup(c.attachmentScope(), draftID)
	if !ok {
		return attachment.DraftCredential{}, nil, attachment.Error{Code: attachment.CodeMissing, Message: "draft credential is not valid", Retry: true}
	}
	raw, err := c.attachmentService().ReadVerified(ctx, draft.Ref)
	return draft, raw, err
}

func (c *Controller) ReleaseDraftImage(draftID string) {
	c.attachmentService().Drafts().Release(c.attachmentScope(), draftID)
}

// ReadSessionAttachment returns one bounded range of an admitted original.
// digest is the only client-supplied identity; size and integrity come from
// this session's content graph.
func (c *Controller) ReadSessionAttachment(ctx context.Context, digest string, offset, length int64) ([]byte, int64, error) {
	if c == nil {
		return nil, 0, attachment.Error{Code: attachment.CodeMissing, Message: "attachment is not authorized for this session"}
	}
	service := c.SessionService()
	ref, bound := c.SessionRef()
	if service == nil || service.Query() == nil || !bound {
		return nil, 0, attachment.Error{Code: attachment.CodeMissing, Message: "attachment is not authorized for this session"}
	}
	return service.Query().ReadSessionAttachment(ctx, ref, digest, offset, length)
}

func (c *Controller) PersistToolImages(ctx context.Context, images []string) ([]attachment.ImageInput, error) {
	if len(images) == 0 {
		return nil, nil
	}
	out := make([]attachment.ImageInput, 0, len(images))
	var sources []attachment.Source
	var slots []int
	for _, image := range images {
		switch provider.ClassifyImage(image) {
		case provider.ImageHTTPURL:
			out = append(out, attachment.ImageInput{Kind: attachment.KindURL, URL: image})
		case provider.ImageFileID:
			out = append(out, attachment.ImageInput{Kind: attachment.KindFiles, FilesID: image})
		default:
			slots = append(slots, len(out))
			out = append(out, attachment.ImageInput{})
			sources = append(sources, attachment.Source{DataURL: image})
		}
	}
	if len(sources) == 0 {
		return out, nil
	}
	svc := c.attachmentService()
	prepared, err := svc.PrepareBatch(ctx, sources)
	if err != nil {
		return nil, err
	}
	refs, err := svc.CommitBatch(ctx, prepared)
	if err != nil {
		return nil, err
	}
	for i, ref := range refs {
		item := ref
		out[slots[i]] = attachment.ImageInput{Kind: attachment.KindAttachment, Attachment: &item}
	}
	return out, nil
}

func (c *Controller) ResolveRequestImages(ctx context.Context, msgs []provider.Message) ([]provider.Message, error) {
	if c == nil {
		return msgs, nil
	}
	return c.ResolveRequestImagesForModel(ctx, msgs, c.selection.ref, c.imageInputEnabled())
}

// ImageRequestRoute is request-local and must never be persisted in history.
type ImageRequestRoute struct {
	Model      string
	BaseURL    string
	APIKey     string
	AuthHeader bool
	Protocol   string
}

func (c *Controller) captureImageRoutes(cfg *config.Config) {
	c.imageRoutes = make(map[string]ImageRequestRoute)
	for _, entry := range cfg.Providers {
		protocol := "openai"
		if strings.EqualFold(entry.Kind, "anthropic") {
			protocol = "anthropic"
		}
		route := ImageRequestRoute{BaseURL: entry.BaseURL, APIKey: entry.APIKey(), AuthHeader: entry.AuthHeader, Protocol: protocol}
		c.imageRoutes[entry.Name] = route
	}
	if prefix, _, ok := strings.Cut(cfg.DefaultModel, "/"); ok {
		c.imageRoutes[""] = c.imageRoutes[prefix]
	}
}

func (c *Controller) imageRequestRoute(model string) (ImageRequestRoute, error) {
	c.imageRoutesOnce.Do(func() {
		cfg, err := config.LoadForRootReadOnly(c.workspaceRoot)
		c.imageRoutesErr = err
		if err == nil {
			c.captureImageRoutes(cfg)
		}
	})
	if c.imageRoutesErr != nil {
		return ImageRequestRoute{}, c.imageRoutesErr
	}
	prefix, _, _ := strings.Cut(model, "/")
	route := c.imageRoutes[prefix]
	route.Model = model
	return route, nil
}

func (c *Controller) ResolveRequestImagesForModel(ctx context.Context, msgs []provider.Message, model string, native bool) ([]provider.Message, error) {
	if c == nil {
		return msgs, nil
	}
	route, err := c.imageRequestRoute(model)
	if err != nil {
		return nil, err
	}
	out := append([]provider.Message(nil), msgs...)
	for i := range out {
		if err := out[i].ValidateImageFields(); err != nil {
			return nil, err
		}
		if len(out[i].ImageInputs) == 0 {
			continue
		}
		if !native {
			if out[i].VisionSummary == nil {
				svc := imageinput.New(imageinput.Config{Model: c.visionModel, Resolve: c.visionProviderResolver, Select: c.visionModelSelector})
				if c.executor != nil && c.executor.ImageInput() != nil {
					svc = c.executor.ImageInput()
				}
				target, err := svc.SelectModel(model, nil)
				if err != nil {
					return nil, err
				}
				visionRoute, err := c.imageRequestRoute(target)
				if err != nil {
					return nil, err
				}
				images, err := c.resolveImageInputsForRoute(ctx, out[i].ImageInputs, visionRoute)
				if err != nil {
					return nil, err
				}
				summary, err := svc.UnderstandSelected(ctx, target, images, nil, c.sink)
				if err != nil {
					return nil, err
				}
				out[i].Content = imageinput.AppendSummary(out[i].Content, summary)
			}
			out[i].ImageInputs = nil
			continue
		}
		resolved, err := c.resolveImageInputsForRoute(ctx, out[i].ImageInputs, route)
		if err != nil {
			return nil, err
		}
		out[i].Images = resolved
		out[i].ImageInputs = nil
	}
	return out, nil
}

func (c *Controller) resolveImageInputsForRoute(ctx context.Context, inputs []attachment.ImageInput, route ImageRequestRoute) ([]string, error) {
	out := make([]string, 0, len(inputs))
	for i, in := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := in.Validate(); err != nil {
			return nil, err
		}
		switch in.Kind {
		case attachment.KindURL:
			out = append(out, in.URL)
		case attachment.KindFiles:
			out = append(out, in.FilesID)
		case attachment.KindAttachment:
			value, err := c.wireImageFromRefForRoute(ctx, *in.Attachment, route)
			if err != nil {
				var item attachment.Error
				if errors.As(err, &item) {
					item.Index = i + 1
					return nil, item
				}
				return nil, err
			}
			out = append(out, value)
		}
	}
	return out, nil
}

func (c *Controller) wireImageFromRefForRoute(ctx context.Context, ref attachment.AttachmentRef, route ImageRequestRoute) (string, error) {
	svc := c.attachmentService()
	variant, err := svc.PrepareVariant(ctx, ref, attachment.VariantPolicyV1)
	if err != nil {
		return "", err
	}
	if len(variant.Bytes) <= inlineImageLimit {
		return attachment.DataURL(variant.MIME, variant.Bytes), nil
	}
	id, err := uploadImageForRoute(ctx, route, ref.DisplayName, variant.Bytes)
	if err == nil {
		return id, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", err
	}
	if len(variant.Bytes) > provider.MaxInlineImageBytes {
		return "", err
	}
	return attachment.DataURL(variant.MIME, variant.Bytes), nil
}

func uploadImageForRoute(ctx context.Context, route ImageRequestRoute, filename string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !openai.IsDeepSeek(route.BaseURL) {
		return "", errFilesAPI()
	}
	return uploadVisionFile(ctx, provider.FileUpload{
		BaseURL:    route.BaseURL,
		APIKey:     route.APIKey,
		AuthHeader: route.AuthHeader,
		Protocol:   route.Protocol,
		Filename:   path.Base(filename),
		Data:       data,
	})
}

func errFilesAPI() error { return fmt.Errorf("files api requires official DeepSeek") }
