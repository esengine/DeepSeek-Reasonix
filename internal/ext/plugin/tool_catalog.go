package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
)

// toolCatalog is one answered tools/list after judgement: what the model is
// offered, what was withheld from it, and the digest that tells a real change
// from the same answer arriving again.
type toolCatalog struct {
	infos       []ToolInfo
	tools       []tool.Tool
	held        []HeldTool
	followed    []HeldTool
	fingerprint [sha256.Size]byte
}

// readToolCatalog is the one path from a tools/list response to registrable
// tools: list, judge against the approved definitions, build. Anything that
// refreshes a catalog must come through it, or it skips the judgement. It takes
// no lock and publishes nothing, so the caller swaps the result in as one
// piece. settle gives a dynamically registering server the startup wait.
func (c *Client) readToolCatalog(ctx context.Context, settle bool) (toolCatalog, error) {
	list := c.listToolsRaw
	if settle {
		list = c.listToolsRawSettled
	}
	listed, err := list(ctx)
	if err != nil {
		return toolCatalog{}, err
	}
	out := slices.DeleteFunc(slices.Clone(listed), func(t mcpTool) bool { return !c.spec.ToolEnabled(t.Name) })
	if err := validateMCPToolNames(c.name, c.spec.StripRawPrefix, out); err != nil {
		return toolCatalog{}, fmt.Errorf("plugin %q: %w", c.name, err)
	}
	held, followed := judgeToolPins(c.spec, listed)
	out = slices.DeleteFunc(out, func(t mcpTool) bool {
		return slices.ContainsFunc(held, func(h HeldTool) bool { return h.RawName == t.Name })
	})
	held = slices.DeleteFunc(held, func(h HeldTool) bool { return !c.spec.ToolEnabled(h.RawName) })

	toolInfos, sortedTools := c.buildToolCatalog(out)
	fingerprint := toolCatalogFingerprint(toolInfos, sortedTools, held)
	if err := c.checkCompositionContract(fingerprint); err != nil {
		return toolCatalog{}, err
	}
	return toolCatalog{
		infos: toolInfos, tools: sortedTools, held: held, followed: followed,
		fingerprint: fingerprint,
	}, nil
}

// publishToolCatalogLocked makes one read current. Everything a catalog
// publishes moves together — including what it withheld, which a surface shows
// beside the tools. The caller holds toolsMu.
func (c *Client) publishToolCatalogLocked(cat toolCatalog) {
	c.tools = cat.infos
	c.held = cat.held
	c.followed = cat.followed
	c.toolAdapters = append([]tool.Tool(nil), cat.tools...)
	c.toolsFingerprint = cat.fingerprint
	c.toolsListed = true
}

// buildToolCatalog turns one tools/list answer into the status rows and the
// model-visible adapters, both name-sorted. It performs no I/O, so a caller can
// build a candidate catalog before taking the lock that publishes it.
func (c *Client) buildToolCatalog(out []mcpTool) ([]ToolInfo, []tool.Tool) {
	toolInfos := make([]ToolInfo, 0, len(out))
	tools := make([]tool.Tool, 0, len(out))
	normalizedSchemas := make(map[string]json.RawMessage, len(out))
	for _, t := range out {
		schema, err := normalizeAndValidateToolSchema(t.InputSchema)
		if err != nil {
			continue
		}
		normalizedSchemas[t.Name] = schema
	}
	for _, t := range out {
		readOnlyHint, destructiveHint := t.hints()
		info := ToolInfo{Name: t.Name, Description: t.Description, ReadOnlyHint: readOnlyHint, DestructiveHint: destructiveHint}
		schema, ok := normalizedSchemas[t.Name]
		if !ok {
			if _, err := normalizeAndValidateToolSchema(t.InputSchema); err != nil {
				info.SchemaError = schemaValidationError(err)
			}
			toolInfos = append(toolInfos, info)
			continue
		}
		headers, err := c.toolParamHeaders(t.InputSchema)
		if err != nil {
			slog.Warn("plugin: tool left out for its header annotations", "server", c.name, "tool", t.Name, "err", secrets.DiagnosticError(err))
			info.SchemaError = err.Error()
			toolInfos = append(toolInfos, info)
			continue
		}
		visibleName := t.Name
		if c.spec.StripRawPrefix != "" {
			visibleName = strings.TrimPrefix(visibleName, c.spec.StripRawPrefix)
		}
		readOnly := readOnlyHint
		toolInfos = append(toolInfos, info)
		tools = append(tools, &remoteTool{
			client:           c,
			name:             toolName(c.name, visibleName),
			rawName:          t.Name,
			visibleName:      visibleName,
			desc:             t.Description,
			schema:           schema,
			outputSchema:     t.OutputSchema,
			declaredReadOnly: readOnlyHint,
			readOnly:         readOnly,
			destructive:      destructiveHint,
			paramHeaders:     headers,
		})
	}
	sort.SliceStable(toolInfos, func(i, j int) bool { return toolInfos[i].Name < toolInfos[j].Name })
	return toolInfos, sortToolsByName(tools)
}
