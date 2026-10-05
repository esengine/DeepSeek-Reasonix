// Package laya runs a Laya decision model as a local process. A self-hosted
// gateway is not here: it speaks the same wire at a different address, so it is
// an ordinary provider entry on the decision protocol.
package laya

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"reasonix/internal/safety/typesafe"
)

const resultMarker = "__REASONIX_LAYA_RESULT__"

type LocalClient struct {
	Python string
	Model  string
}

func (c LocalClient) Evaluate(ctx context.Context, request typesafe.Request) (typesafe.Response, error) {
	python := strings.TrimSpace(c.Python)
	if python == "" {
		python = "python"
	}
	model := strings.TrimSpace(c.Model)
	if model != "" && model != "auto" {
		request.Model = model
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return typesafe.Response{}, err
	}
	cmd := exec.CommandContext(ctx, python, "-u", "-c", localBridge)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return typesafe.Response{}, fmt.Errorf("run local Laya (%s): %w: %s", python, err, strings.TrimSpace(string(output)))
	}
	index := bytes.LastIndex(output, []byte(resultMarker))
	if index < 0 {
		return typesafe.Response{}, errors.New("local Laya returned no decision result")
	}
	var result typesafe.Response
	if err := json.Unmarshal(bytes.TrimSpace(output[index+len(resultMarker):]), &result); err != nil {
		return typesafe.Response{}, fmt.Errorf("decode local Laya response: %w", err)
	}
	return result, nil
}

// localBridge drops the working directory `python -c` puts first on sys.path,
// so `import laya` never resolves to a module a checkout or a jailed command
// placed where the host happens to run.
const localBridge = `
import json, sys
if sys.path and sys.path[0] == "":
    del sys.path[0]
from laya import Router

request = json.load(sys.stdin)
router = Router()
model = request.get("model")
if model in (None, "", "auto", "laya-auto"):
    model = None
result = router.predict(request["state"], request["questions"], model=model)
if "model" not in result:
    result["model"] = result.get("routing", {}).get("model", model or "auto")
print("__REASONIX_LAYA_RESULT__" + json.dumps(result, ensure_ascii=False))
`
