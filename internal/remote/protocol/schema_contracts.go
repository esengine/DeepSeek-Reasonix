package protocol

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

var protocolValidatableType = reflect.TypeOf((*protocolValidatable)(nil)).Elem()

var customSchemaContracts = buildCustomSchemaContracts()

func buildCustomSchemaContracts() map[reflect.Type]SchemaValidation {
	contracts := map[reflect.Type]SchemaValidation{
		typeOf[BuildID]():               {Invariants: rules("productVersion:trimmed_nonempty", "sourceRevision:full_lowercase_git_commit_with_optional_dirty", "protocolVersion:trimmed_nonempty", "schemaHash:sha256_prefixed_lowercase_hex")},
		typeOf[RuntimeTarget]():         {Invariants: rules("workspaceId:trimmed_nonempty", "sessionId:trimmed_nonempty")},
		typeOf[WorkspaceBrowseParams](): {Invariants: rules("mutually_exclusive(directoryRef,typedPath)")},
		typeOf[ProfilePatch]():          {Invariants: rules("at_least_one(model,effort,collaborationMode,tokenMode,toolApprovalMode)")},
		typeOf[TopicSelection](): {Discriminator: discriminator("kind",
			variant([]string{string(TopicExisting)}, []string{"topicId"}, []string{"title"}, nil, nil),
			variant([]string{string(TopicNew)}, nil, []string{"topicId"}, nil, nil),
		)},
		typeOf[TopicCreateResult](): {Invariants: rules("sessionCount=0")},
		typeOf[PendingPrompt](): {Discriminator: discriminator("kind",
			variant([]string{string(PromptApproval)}, []string{"approval"}, []string{"ask"}, nil, nil),
			variant([]string{string(PromptAsk)}, []string{"ask"}, []string{"approval"}, nil, nil),
		)},
		typeOf[SessionSubmitParams](): {
			Invariants: rules("deliveryRecovery:forbids(editedOriginal,invocations)", "editedOriginal:forbids(invocations)"),
		},
		typeOf[SessionSubmitResult](): {
			Invariants: rules("snapshotRequired:false_for(kind=turn|operation|effect=none),true_for(effect=runtime_replaced|session_replaced),optional_for(effect=state_changed)"),
			Discriminator: discriminator("kind",
				variant([]string{string(SubmitTurn)}, []string{"turnId"}, []string{"operationId", "operation", "effect"}, nil, nil),
				variant([]string{string(SubmitOperation)}, []string{"operationId", "operation"}, []string{"turnId", "effect"}, nil, nil),
				variant([]string{string(SubmitCompleted)}, []string{"effect"}, []string{"turnId", "operationId", "operation"}, nil, nil),
			),
		},
		typeOf[PromptAnswerParams]():     {Invariants: rules("answers.questionId:unique")},
		typeOf[GitCommitDetailParams]():  {Invariants: rules("path:forbids(cursor,limit)")},
		typeOf[ResearchListParams]():     {Invariants: rules("cursor:empty_or_canonical_signed_research_cursor_shape")},
		typeOf[ResearchFindingsParams](): {Invariants: rules("cursor:empty_or_canonical_signed_research_cursor_shape")},
		typeOf[GitCommitDetailResult](): {
			Invariants: rules("patch:returnedBytes<=sizeBytes", "patch:truncated=iff(sizeBytes>returnedBytes)", "patch:truncationReason=byte_limit_iff_truncated", "files:hasMore=iff(nextCursor_present)"),
			Discriminator: discriminator("kind",
				variant([]string{string(GitDetailFiles)}, []string{"files", "hasMore"}, []string{"path", "body", "sizeBytes", "returnedBytes", "truncated", "truncationReason"}, nil, nil),
				variant([]string{string(GitDetailPatch)}, []string{"path", "body", "sizeBytes", "returnedBytes", "truncated"}, []string{"files", "hasMore", "nextCursor"}, nil, nil),
			),
		},
		typeOf[WorkspaceChangeDetailResult](): {
			Invariants: rules("source_absent:forbids(diff,added,removed,binary,truncated)", "truncated:requires(source)", "truncated:forbids(diff,added,removed,binary)"),
		},
		typeOf[FilePreviewResult](): {
			Invariants: rules("text:returnedBytes<=sizeBytes", "text:truncated=iff(sizeBytes>returnedBytes)", "text:truncationReason=byte_limit_iff_truncated", "nontext:returnedBytes=0", "nontext:truncated=false"),
			Discriminator: discriminator("kind",
				variant([]string{string(FileText)}, []string{"body"}, nil, nil, []string{"binary"}),
				variant([]string{string(FileBinary), string(FileImage), string(FilePDF)}, nil, []string{"body", "truncationReason"}, []string{"binary"}, []string{"truncated"}),
			),
		},
		typeOf[LeaseInfo]():            {Invariants: rules("ttlMs=30000", "pingIntervalMs=10000")},
		typeOf[PingResult]():           {Invariants: rules("leaseTtlMs=30000")},
		typeOf[ExternalizedField]():    {Invariants: rules("totalBytes<=8388608", "truncated:requires(originalBytes>totalBytes,truncationReason)", "not_truncated:forbids(originalBytes,truncationReason)")},
		typeOf[HistoryPage]():          {Invariants: rules("0<=startTurn<=endTurn<=totalTurns", "actualTurns=endTurn-startTurn", "hasOlder=iff(startTurn>0)", "hasOlder=iff(nextCursor_present)", "externalized.jsonPointer:unique_and_schema_marked")},
		typeOf[SessionContentResult](): {Invariants: rules("dataBase64:valid_base64", "decodedBytes<=262144", "offset+decodedBytes<=totalBytes<=8388608", "nonfinal:nextOffset=offset+decodedBytes", "final:nextOffset_absent")},
		typeOf[SessionEvent]():         {Invariants: rules("seq>=1", "at_most_one(turnId,operationId)", "event.kind:eventwire_registered", "externalized.jsonPointer:unique_and_schema_marked")},
		typeOf[SessionSnapshot]():      {Invariants: rules("history.snapshotId=snapshotId", "runtime.liveEvents.kind:eventwire_registered", "externalized.jsonPointer:unique_and_schema_marked")},
		typeOf[SessionResyncRequired](): {Discriminator: discriminator("reason",
			variant([]string{string(ResyncQueueOverflow), string(ResyncStateChanged)}, nil, []string{"replacementTarget", "replacementRuntimeEpoch"}, nil, nil),
			variant([]string{string(ResyncRuntimeReplaced)}, []string{"replacementRuntimeEpoch"}, []string{"replacementTarget"}, nil, nil),
			variant([]string{string(ResyncTargetReplaced)}, []string{"replacementTarget", "replacementRuntimeEpoch"}, nil, nil, nil),
		)},
		typeOf[CatalogChanged](): {
			Invariants: rules("kinds:nonempty_unique", "affectedWorkspaceIds:unique"),
			Discriminator: discriminator("scope",
				variant([]string{string(CatalogHost)}, nil, []string{"affectedWorkspaceIds"}, nil, nil),
				variant([]string{string(CatalogWorkspace)}, []string{"affectedWorkspaceIds"}, nil, nil, nil),
			),
		},
		typeOf[FileSearchResult]():        {Invariants: rules("returnedItems=len(entries)", "totalItems>=returnedItems", "truncated=iff(truncationReason_present)")},
		typeOf[GitHistoryResult]():        {Invariants: rules("returnedItems=len(commits)", "truncated=iff(truncationReason=history_limit)")},
		typeOf[Capabilities]():            {Invariants: rules("features.coreSession=true", "features.primaryFileQueries=true", "features.userShell=true", "features.jobCancel=true", "features.memory:dynamic", "features.research:dynamic", "deferred_features=false", "limits=frozen_remote_v1")},
		typeOf[RemoteErrorData]():         {Invariants: rules("reasonixCode:selects_frozen_retryable_action_command", "expected:paired_with(actual)", "expected_actual:bounded_path_free_tokens", "HOST_BUSY:requires_nonnegative(retryAfterMs)", "other_errors:forbid(retryAfterMs)", "REWIND_PARTIAL:requires(snapshotRequired=true,at_least_one_change_flag=true)", "other_errors:forbid(change_flags,snapshotRequired)")},
		typeOf[BrokerProviderRequest]():   {Invariants: rules("messages:non_null_array", "tools:non_null_array", "tools.parameters:valid_json_object", "maxTokens>=0")},
		typeOf[BrokerProviderChunk]():     {Invariants: rules("argChars>=0", "type=error:requires(error)", "type!=error:forbids(error)", "type=usage:requires(usage)")},
		typeOf[BrokerStreamOpenParams]():  {Invariants: rules("streamId:trimmed_nonempty", "providerRef:trimmed_nonempty", "request:typed_provider_request")},
		typeOf[BrokerStreamChunkParams](): {Invariants: rules("streamId:trimmed_nonempty", "seq>=1", "chunk:typed_provider_chunk")},
	}
	page := SchemaValidation{Invariants: rules("hasMore=iff(nextCursor_present)")}
	for _, typ := range []reflect.Type{
		typeOf[WorkspaceBrowseResult](), typeOf[WorkspaceListResult](), typeOf[SessionListResult](),
		typeOf[TopicListResult](), typeOf[SessionTrashListResult](), typeOf[ComposerHistoryResult](),
		typeOf[ResearchListResult](), typeOf[ResearchFindingsResult](), typeOf[FileListResult](),
		typeOf[WorkspaceChangesResult](), typeOf[JobListResult](),
	} {
		contracts[typ] = page
	}
	return contracts
}

func rules(values ...string) []string { return values }

func discriminator(property string, variants ...SchemaVariant) *SchemaDiscriminator {
	return &SchemaDiscriminator{Property: property, Variants: variants}
}

func variant(values, required, forbidden, requiredTrue, requiredFalse []string) SchemaVariant {
	return SchemaVariant{Values: values, Required: required, Forbidden: forbidden, RequiredTrue: requiredTrue, RequiredFalse: requiredFalse}
}

func schemaValidationFor(typ reflect.Type) (*SchemaValidation, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	implementsValidation := typ.Implements(protocolValidatableType) || (typ.Kind() != reflect.Pointer && reflect.PointerTo(typ).Implements(protocolValidatableType))
	contract, hasContract := customSchemaContracts[typ]
	if implementsValidation && !hasContract {
		return nil, fmt.Errorf("custom wire validation for %v has no deterministic schema contract", typ)
	}
	if !hasContract {
		return nil, nil
	}
	if !implementsValidation {
		return nil, fmt.Errorf("schema contract for %v has no custom validator", typ)
	}
	normalized := normalizeSchemaValidation(contract)
	return &normalized, nil
}

func normalizeSchemaValidation(in SchemaValidation) SchemaValidation {
	out := SchemaValidation{Invariants: append([]string(nil), in.Invariants...)}
	sort.Strings(out.Invariants)
	if in.Discriminator != nil {
		out.Discriminator = &SchemaDiscriminator{Property: in.Discriminator.Property, Variants: append([]SchemaVariant(nil), in.Discriminator.Variants...)}
		for i := range out.Discriminator.Variants {
			variant := &out.Discriminator.Variants[i]
			variant.Values = sortedCopy(variant.Values)
			variant.Required = sortedCopy(variant.Required)
			variant.Forbidden = sortedCopy(variant.Forbidden)
			variant.RequiredTrue = sortedCopy(variant.RequiredTrue)
			variant.RequiredFalse = sortedCopy(variant.RequiredFalse)
		}
		sort.Slice(out.Discriminator.Variants, func(i, j int) bool {
			return strings.Join(out.Discriminator.Variants[i].Values, "\x00") < strings.Join(out.Discriminator.Variants[j].Values, "\x00")
		})
	}
	return out
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func validateSchemaContract(schema SchemaType, validation *SchemaValidation) error {
	if validation == nil {
		return nil
	}
	seenRules := make(map[string]bool, len(validation.Invariants))
	for _, invariant := range validation.Invariants {
		if strings.TrimSpace(invariant) == "" || seenRules[invariant] {
			return fmt.Errorf("invariants must be non-empty and unique")
		}
		seenRules[invariant] = true
	}
	if validation.Discriminator == nil {
		return nil
	}
	properties := make(map[string]SchemaType, len(schema.Properties))
	for _, property := range schema.Properties {
		properties[property.Name] = property.Schema
	}
	discriminatorSchema, ok := properties[validation.Discriminator.Property]
	if !ok || len(discriminatorSchema.Enum) == 0 {
		return fmt.Errorf("discriminator %q must name an enum property", validation.Discriminator.Property)
	}
	allowedValues := make(map[string]bool, len(discriminatorSchema.Enum))
	for _, value := range discriminatorSchema.Enum {
		allowedValues[value] = true
	}
	coveredValues := make(map[string]bool, len(allowedValues))
	for _, variant := range validation.Discriminator.Variants {
		if len(variant.Values) == 0 {
			return fmt.Errorf("discriminator variant has no values")
		}
		for _, value := range variant.Values {
			if !allowedValues[value] || coveredValues[value] {
				return fmt.Errorf("discriminator value %q is unknown or duplicated", value)
			}
			coveredValues[value] = true
		}
		groups := [][]string{variant.Required, variant.Forbidden, variant.RequiredTrue, variant.RequiredFalse}
		seenProperties := make(map[string]bool)
		for _, group := range groups {
			for _, property := range group {
				propertySchema, exists := properties[property]
				if !exists || seenProperties[property] {
					return fmt.Errorf("variant property %q is unknown or contradictory", property)
				}
				seenProperties[property] = true
				if (contains(variant.RequiredTrue, property) || contains(variant.RequiredFalse, property)) && propertySchema.Type != "boolean" {
					return fmt.Errorf("literal property %q is not boolean", property)
				}
			}
		}
	}
	if len(coveredValues) != len(allowedValues) {
		return fmt.Errorf("discriminator variants do not cover the enum")
	}
	return nil
}
