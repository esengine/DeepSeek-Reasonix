package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/provider"
)

type protocolValidatable interface {
	Validate() error
}

type validationFailure struct{ message string }

func (e *validationFailure) Error() string { return e.message }

func validationError(message string) error { return &validationFailure{message: message} }

var (
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	gitHashPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

var opaqueTypes = map[reflect.Type]struct{}{
	reflect.TypeOf(WorkspaceID("")): {}, reflect.TypeOf(SessionID("")): {},
	reflect.TypeOf(RequestID("")): {}, reflect.TypeOf(HostEpoch("")): {},
	reflect.TypeOf(RuntimeEpoch("")): {}, reflect.TypeOf(TurnID("")): {},
	reflect.TypeOf(OperationID("")): {}, reflect.TypeOf(PromptID("")): {},
	reflect.TypeOf(CheckpointID("")): {}, reflect.TypeOf(SubscriptionID("")): {},
	reflect.TypeOf(SnapshotID("")): {}, reflect.TypeOf(ContentRef("")): {},
	reflect.TypeOf(Cursor("")): {}, reflect.TypeOf(LeaseID("")): {},
	reflect.TypeOf(ClientInstanceID("")): {}, reflect.TypeOf(DirectoryRef("")): {},
	reflect.TypeOf(TopicID("")): {}, reflect.TypeOf(CatalogRevision("")): {},
	reflect.TypeOf(MemoryID("")): {}, reflect.TypeOf(DocumentID("")): {},
	reflect.TypeOf(SuggestionID("")): {}, reflect.TypeOf(SkillID("")): {},
	reflect.TypeOf(ResearchTaskID("")): {}, reflect.TypeOf(CriterionID("")): {},
	reflect.TypeOf(JobID("")): {}, reflect.TypeOf(QuestionID("")): {},
	reflect.TypeOf(ModelRef("")): {},
}

var enumTypes = map[reflect.Type][]string{
	reflect.TypeOf(Direction("")):                  values(DirectionClientRequest, DirectionHostNotification),
	reflect.TypeOf(OperationClass("")):             values(ClassConnection, ClassHostQuery, ClassHostMutation, ClassSessionQuery, ClassSessionMutation, ClassSessionRecordMutation, ClassHostNotification),
	reflect.TypeOf(RemoteAction("")):               values(ActionNone, ActionRetry, ActionReconnect, ActionResubscribe, ActionRestartDaemon, ActionRunCommand),
	reflect.TypeOf(InvocationKind("")):             values(InvocationSkill, InvocationSubagent),
	reflect.TypeOf(SubmitKind("")):                 values(SubmitTurn, SubmitOperation, SubmitCompleted),
	reflect.TypeOf(OperationKind("")):              values(OperationShell, OperationCompact, OperationSummarize),
	reflect.TypeOf(SubmitEffect("")):               values(EffectNone, EffectStateChanged, EffectRuntimeReplaced, EffectSessionReplaced),
	reflect.TypeOf(CancelStatus("")):               values(CancelRequested, CancelAlreadyRequested),
	reflect.TypeOf(PromptDecision("")):             values(DecisionAllowOnce, DecisionAllowSession, DecisionAllowPersistent, DecisionDeny),
	reflect.TypeOf(PromptKind("")):                 values(PromptApproval, PromptAsk),
	reflect.TypeOf(RewindScope("")):                values(RewindCode, RewindConversation, RewindBoth),
	reflect.TypeOf(SummaryDirection("")):           values(SummaryFrom, SummaryUpTo),
	reflect.TypeOf(GoalStatus("")):                 values(GoalRunning, GoalComplete, GoalBlocked, GoalStopped),
	reflect.TypeOf(CollaborationMode("")):          values(CollaborationNormal, CollaborationPlan, CollaborationGoal),
	reflect.TypeOf(TokenMode("")):                  values(TokenFull, TokenEconomy, TokenDelivery),
	reflect.TypeOf(ToolApprovalMode("")):           values(ToolApprovalAsk, ToolApprovalAuto, ToolApprovalYOLO),
	reflect.TypeOf(TopicSelectionKind("")):         values(TopicExisting, TopicNew),
	reflect.TypeOf(TrashGuard("")):                 values(TrashNormal, TrashRedundantRecoveryOnly),
	reflect.TypeOf(CatalogScope("")):               values(CatalogHost, CatalogWorkspace),
	reflect.TypeOf(CatalogKind("")):                values(CatalogTopics, CatalogSessions, CatalogTrash, CatalogWorkspaceCatalog, CatalogSessionCatalog, CatalogMemory, CatalogResearch),
	reflect.TypeOf(ResyncReason("")):               values(ResyncQueueOverflow, ResyncRuntimeReplaced, ResyncTargetReplaced, ResyncStateChanged),
	reflect.TypeOf(SessionOutcome("")):             values(OutcomeCompleted, OutcomeCancelled, OutcomeFailed, OutcomeInterrupted),
	reflect.TypeOf(InterruptionReason("")):         values(InterruptionHostRestarted),
	reflect.TypeOf(FileKind("")):                   values(FileText, FileBinary, FileImage, FilePDF),
	reflect.TypeOf(SearchTruncationReason("")):     values(SearchResultLimit, SearchScanLimit),
	reflect.TypeOf(ByteTruncationReason("")):       values(ByteLimit),
	reflect.TypeOf(GitHistoryTruncationReason("")): values(GitHistoryLimit),
	reflect.TypeOf(GitCommitDetailKind("")):        values(GitDetailFiles, GitDetailPatch),
	reflect.TypeOf(ChangeSource("")):               values(ChangeSession, ChangeGit),
	reflect.TypeOf(JobKind("")):                    values(JobBash, JobTask),
	reflect.TypeOf(JobStatus("")):                  values(JobRunning),
	reflect.TypeOf(JobCancelDisposition("")):       values(JobCancelled, JobNotRunning),
	reflect.TypeOf(ContentEncoding("")):            values(ContentUTF8),
	reflect.TypeOf(TodoStatus("")):                 values(TodoPending, TodoInProgress, TodoCompleted),
	reflect.TypeOf(WorkspaceOpenDisposition("")):   values(WorkspaceOpened, WorkspaceAlreadyOpen),
	reflect.TypeOf(WorkspaceCloseDisposition("")):  values(WorkspaceClosed, WorkspaceAlreadyClosed),
	reflect.TypeOf(SessionCloseDisposition("")):    values(SessionReleased, SessionRetainedActive, SessionAlreadyClosed),
	reflect.TypeOf(CleanupDisposition("")):         values(DispositionTrashed, DispositionCleanupPending, DispositionAlreadyTrashed),
	reflect.TypeOf(SessionRestoreDisposition("")):  values(SessionRestored),
	reflect.TypeOf(SessionClearDisposition("")):    values(SessionCleared, SessionCleanupPending),
	reflect.TypeOf(ProfileSetDisposition("")):      values(ProfileUpdated, ProfileRebuilt),
	reflect.TypeOf(provider.Role("")):              values(provider.RoleSystem, provider.RoleUser, provider.RoleAssistant, provider.RoleTool),
	reflect.TypeOf(BrokerChunkType("")):            values(BrokerChunkText, BrokerChunkReasoning, BrokerChunkToolCallStart, BrokerChunkToolCallDelta, BrokerChunkToolCall, BrokerChunkUsage, BrokerChunkDone, BrokerChunkError),
	reflect.TypeOf(BrokerProviderErrorCode("")):    values(BrokerProviderFailed, BrokerProviderInterrupted),
}

func init() {
	contracts := ErrorContracts()
	codes := make([]string, len(contracts))
	for i := range contracts {
		codes[i] = string(contracts[i].ReasonixCode)
	}
	enumTypes[reflect.TypeOf(ReasonixErrorCode(""))] = codes
}

func values[T ~string](in ...T) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = string(in[i])
	}
	return out
}

func decodeAndValidate(raw json.RawMessage, typ reflect.Type) (any, error) {
	if typ.Kind() != reflect.Struct {
		return nil, errors.New("protocol registry params must be structs")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := validateRequiredJSON(raw, typ, "params"); err != nil {
		return nil, err
	}
	ptr := reflect.New(typ)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(ptr.Interface()); err != nil {
		return nil, validationError("params do not match the registered type")
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, validationError("params contain trailing JSON")
	}
	value := ptr.Elem().Interface()
	if err := validateDecoded(value); err != nil {
		return nil, err
	}
	return value, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("extra JSON value")
	}
	return err
}

func validateRequiredJSON(raw json.RawMessage, typ reflect.Type, at string) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return validationError(at + " must be a JSON object")
	}
	return validateRequiredObject(object, typ, at)
}

func validateRequiredObject(object map[string]json.RawMessage, typ reflect.Type, at string) error {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, omitEmpty, skip := jsonField(field)
		if skip {
			continue
		}
		if field.Anonymous && name == "" {
			if err := validateRequiredObject(object, field.Type, at); err != nil {
				return err
			}
			continue
		}
		fieldRaw, present := object[name]
		if !omitEmpty && !present {
			return validationError(fmt.Sprintf("%s.%s is required", at, name))
		}
		if !present {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(fieldRaw), []byte("null")) {
			if field.Tag.Get("nullable") == "true" || field.Tag.Get("externalizable") == "true" {
				continue
			}
			return validationError(fmt.Sprintf("%s.%s must not be null", at, name))
		}
		if err := validateNestedRequired(fieldRaw, field.Type, at+"."+name); err != nil {
			return err
		}
	}
	return nil
}

func validateNestedRequired(raw json.RawMessage, typ reflect.Type, at string) error {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
			return validationError(at + " must contain valid JSON")
		}
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		return validateRequiredJSON(raw, typ, at)
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil
		}
		for i, item := range items {
			if err := validateNestedRequired(item, typ.Elem(), at+"["+strconv.Itoa(i)+"]"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDecoded(value any) error {
	if err := validateValue(reflect.ValueOf(value), "params", false); err != nil {
		return err
	}
	if validatable, ok := value.(protocolValidatable); ok {
		return validatable.Validate()
	}
	return nil
}

func validateValue(value reflect.Value, at string, omitEmpty bool) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		return validateValue(value.Elem(), at, omitEmpty)
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return validateValue(value.Elem(), at, false)
	}
	typ := value.Type()
	if typ == reflect.TypeOf(json.RawMessage{}) {
		raw := value.Interface().(json.RawMessage)
		if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
			return validationError(at + " must contain valid JSON")
		}
		return nil
	}
	if _, opaque := opaqueTypes[typ]; opaque {
		if strings.TrimSpace(value.String()) == "" && !omitEmpty {
			return validationError(at + " must be a non-empty opaque string")
		}
		return nil
	}
	if allowed, enum := enumTypes[typ]; enum {
		if value.String() == "" && omitEmpty {
			return nil
		}
		if !contains(allowed, value.String()) {
			return validationError(fmt.Sprintf("%s has invalid enum value %q", at, value.String()))
		}
		return nil
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name, fieldOmitEmpty, skip := jsonField(field)
			if skip {
				continue
			}
			childAt := at
			if name != "" {
				childAt += "." + name
			}
			if err := validateValue(value.Field(i), childAt, fieldOmitEmpty); err != nil {
				return err
			}
			if err := validateTag(value.Field(i), field.Tag.Get("validate"), childAt, fieldOmitEmpty); err != nil {
				return err
			}
			child := value.Field(i)
			if child.Kind() == reflect.Pointer && child.IsNil() {
				continue
			}
			if child.Kind() == reflect.Pointer {
				child = child.Elem()
			}
			if child.CanInterface() {
				if validatable, ok := child.Interface().(protocolValidatable); ok {
					if err := validatable.Validate(); err != nil {
						return validationError(childAt + ": " + err.Error())
					}
				}
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := validateValue(value.Index(i), fmt.Sprintf("%s[%d]", at, i), false); err != nil {
				return err
			}
			item := value.Index(i)
			if item.Kind() == reflect.Pointer && !item.IsNil() {
				item = item.Elem()
			}
			if item.CanInterface() {
				if validatable, ok := item.Interface().(protocolValidatable); ok {
					if err := validatable.Validate(); err != nil {
						return validationError(fmt.Sprintf("%s[%d]: %v", at, i, err))
					}
				}
			}
		}
	}
	return nil
}

func validateTag(value reflect.Value, tags, at string, omitEmpty bool) error {
	if tags == "" || (omitEmpty && value.IsZero()) {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	for _, tag := range strings.Split(tags, ",") {
		switch {
		case tag == "nonempty":
			if value.Kind() == reflect.String && strings.TrimSpace(value.String()) == "" {
				return validationError(at + " must be non-empty")
			}
		case tag == "true":
			if value.Kind() != reflect.Bool || !value.Bool() {
				return validationError(at + " must be true")
			}
		case strings.HasPrefix(tag, "const="):
			if value.Kind() != reflect.String || value.String() != strings.TrimPrefix(tag, "const=") {
				return validationError(at + " has an invalid fixed value")
			}
		case strings.HasPrefix(tag, "min="):
			minimum, _ := strconv.ParseFloat(strings.TrimPrefix(tag, "min="), 64)
			if numericValue(value) < minimum {
				return validationError(at + " is below its minimum")
			}
		case strings.HasPrefix(tag, "max="):
			maximum, _ := strconv.ParseFloat(strings.TrimPrefix(tag, "max="), 64)
			if numericValue(value) > maximum {
				return validationError(at + " exceeds its maximum")
			}
		case tag == "sha256":
			if !sha256Pattern.MatchString(value.String()) {
				return validationError(at + " must be a lowercase SHA-256 hex value")
			}
		case tag == "gitHash":
			if !gitHashPattern.MatchString(value.String()) {
				return validationError(at + " must be a full lowercase Git commit hash")
			}
		case tag == "rfc3339":
			if _, err := time.Parse(time.RFC3339, value.String()); err != nil {
				return validationError(at + " must be RFC 3339")
			}
		case tag == "jsonPointer":
			if !validJSONPointer(value.String()) {
				return validationError(at + " must be a non-empty RFC 6901 pointer")
			}
		case tag == "relativePath":
			if err := validateRelativePath(value.String()); err != nil {
				return validationError(at + ": " + err.Error())
			}
		case tag == "controlledCommand":
			if !allowedCLICommand(value.String()) {
				return validationError(at + " is not a controlled CLI command")
			}
		}
	}
	return nil
}

func validJSONPointer(pointer string) bool {
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return false
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] != '~' {
			continue
		}
		if i+1 >= len(pointer) || (pointer[i+1] != '0' && pointer[i+1] != '1') {
			return false
		}
		i++
	}
	return true
}

func numericValue(value reflect.Value) float64 {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint())
	case reflect.Float32, reflect.Float64:
		return value.Float()
	}
	return 0
}

func validateRelativePath(value string) error {
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return errors.New("must be a primary-relative POSIX-like path")
	}
	cleaned := path.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("must not escape the primary workspace")
	}
	return nil
}

func allowedCLICommand(command string) bool {
	switch command {
	case "reasonix remote install", "reasonix remote start", "reasonix remote stop",
		"reasonix remote restart", "reasonix remote status", "reasonix remote doctor",
		"reasonix remote logs", "reasonix remote uninstall", "reasonix setup":
		return true
	default:
		return false
	}
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func jsonField(field reflect.StructField) (name string, omitEmpty, skip bool) {
	tag := field.Tag.Get("json")
	parts := strings.Split(tag, ",")
	if len(parts) > 0 && parts[0] == "-" {
		return "", false, true
	}
	if len(parts) > 0 {
		name = parts[0]
	}
	for _, option := range parts[1:] {
		if option == "omitempty" || option == "omitzero" {
			omitEmpty = true
		}
	}
	if name == "" && !field.Anonymous {
		name = field.Name
		name = strings.ToLower(name[:1]) + name[1:]
	}
	return name, omitEmpty, false
}
