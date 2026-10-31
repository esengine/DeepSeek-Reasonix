package builtin

import (
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
)

// CodeMaskedSecretWrite identifies a file write refused for carrying the mark a
// model reads in place of a secret the user saved.
const CodeMaskedSecretWrite = "secret.masked_write"

// CodeMaskedSecretCommand identifies a shell command refused for carrying one.
const CodeMaskedSecretCommand = "secret.masked_command"

// refuseMaskedWrite stops a write that would put a secret's mark where the
// file holds its value: a model that read a masked copy writes the mark back.
func refuseMaskedWrite(args json.RawMessage) error {
	labels := secrets.Masker().MarksJSON(args)
	if len(labels) == 0 {
		return nil
	}
	vars := make([]string, len(labels))
	for i, l := range labels {
		vars[i] = "$REASONIX_SECRET_" + l
	}
	return tool.Refusal{Code: CodeMaskedSecretWrite, Message: fmt.Sprintf(
		"write refused: the text holds a [secret:...] mark, which you read in place of a secret the user saved (%s); the file holds the real value, and writing the mark would replace it. Leave those lines as they are and edit around them; to remove one, use delete_range or a shell command. A shell command reads the value as %s, whichever you were given.",
		strings.Join(labels, ", "), strings.Join(vars, " or "))}
}

// refuseMaskedCommand stops a shell command carrying a secret's mark: written
// to a file or sent as a credential, the mark goes where the value belongs.
func refuseMaskedCommand(args json.RawMessage) error {
	labels := secrets.Masker().MarksJSON(args)
	if len(labels) == 0 {
		return nil
	}
	vars := make([]string, len(labels))
	for i, l := range labels {
		vars[i] = "$REASONIX_SECRET_" + l
	}
	return tool.Refusal{Code: CodeMaskedSecretCommand, Message: fmt.Sprintf(
		"command refused: it holds a [secret:...] mark, which you read in place of a secret the user saved (%s); the command would act on the mark, not the value. Read the value in the command as %s, whichever you were given, and never print it.",
		strings.Join(labels, ", "), strings.Join(vars, " or "))}
}
