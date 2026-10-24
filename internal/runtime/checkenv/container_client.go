package checkenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/fileutil"
)

type containerClient struct {
	executable string
	endpoint   string
	config     string
	version    fileutil.ContentVersion
	engine     containerEngine
	systemRoot string
}

type containerEngine struct {
	ID            string
	ServerVersion string
	KernelVersion string
	OSType        string
	Architecture  string
}

func newContainerClient(ctx context.Context, directory string, spec ClientSpec) (*containerClient, error) {
	if !filepath.IsAbs(spec.Executable) {
		return nil, ErrUnsupported
	}
	executable, err := filepath.EvalSymlinks(spec.Executable)
	if err != nil {
		return nil, err
	}
	version, err := fileutil.ReadContentVersion(ctx, executable)
	if err != nil || version.Kind != "file" {
		return nil, errors.Join(ErrUnsupported, err)
	}
	endpoint, err := containerEndpoint(ctx, executable, directory, spec.Environment)
	if err != nil {
		return nil, err
	}
	client := &containerClient{executable: executable, endpoint: endpoint, config: filepath.Join(directory, "client"), version: version}
	client.systemRoot = containerEnv(spec.Environment, "SystemRoot")
	if err := os.Mkdir(client.config, 0700); err != nil {
		return nil, err
	}
	if err := client.decode(ctx, &client.engine, "info", "--format", "{{json .}}"); err != nil {
		return nil, err
	}
	if client.engine.ID == "" || client.engine.OSType != "linux" || containerArchitecture(client.engine.Architecture) == "" {
		return nil, ErrUnsupported
	}
	return client, nil
}

func containerEndpoint(ctx context.Context, executable, directory string, environment []string) (string, error) {
	endpoint := containerEnv(environment, "DOCKER_HOST")
	if endpoint != "" && containerEnv(environment, "DOCKER_CONTEXT") != "" {
		return "", errors.Join(ErrUnsupported, errors.New("container verification requires an unambiguous Docker endpoint"))
	}
	if endpoint == "" {
		cmd := exec.CommandContext(ctx, executable, "context", "inspect")
		cmd.Dir = directory
		cmd.Env = append([]string{}, environment...)
		body, err := boundedContainerOutput(cmd, nil)
		if err != nil {
			return "", errors.Join(ErrUnsupported, err)
		}
		var contexts []struct {
			Endpoints map[string]struct{ Host string }
		}
		if err := json.Unmarshal(body, &contexts); err != nil || len(contexts) != 1 {
			return "", errors.Join(ErrUnsupported, err)
		}
		endpoint = contexts[0].Endpoints["docker"].Host
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.Join(ErrUnsupported, err)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrUnsupported
	}
	switch parsed.Scheme {
	case "unix":
		if runtime.GOOS == "windows" || parsed.Host != "" || !filepath.IsAbs(parsed.Path) {
			return "", ErrUnsupported
		}
	case "npipe":
		name, local := strings.CutPrefix(parsed.Path, "//./pipe/")
		if runtime.GOOS != "windows" || parsed.Host != "" || !local || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") {
			return "", ErrUnsupported
		}
	default:
		return "", errors.Join(ErrUnsupported, errors.New("container verification requires a local Unix socket or named pipe"))
	}
	return endpoint, nil
}

func containerArchitecture(value string) string {
	switch value {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return ""
	}
}

func (c *containerClient) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.executable, append([]string{"--config", c.config, "--host", c.endpoint}, args...)...)
	cmd.Dir = c.config
	cmd.Env = []string{"LANG=C", "LC_ALL=C", "TMP=" + c.config, "TEMP=" + c.config, "TMPDIR=" + c.config}
	if runtime.GOOS == "windows" {
		cmd.Env = append(cmd.Env, "SystemRoot="+c.systemRoot)
	}
	return cmd
}

func containerEnv(environment []string, name string) string {
	for _, entry := range slices.Backward(environment) {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

const containerCommandTimeout = 20 * time.Second

func (c *containerClient) run(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	return c.runWithin(ctx, containerCommandTimeout, input, args...)
}

// transferTimeout bounds an engine call that moves size bytes at no less than
// 8 MiB/s on top of the ordinary command allowance.
func transferTimeout(size int64) time.Duration {
	return containerCommandTimeout + time.Duration(size/(8<<20))*time.Second
}

func (c *containerClient) runWithin(ctx context.Context, timeout time.Duration, input io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := boundedContainerOutput(c.command(ctx, args...), input)
	if err != nil {
		return nil, fmt.Errorf("controlled container %s: %w", args[0], err)
	}
	return body, nil
}

func (c *containerClient) decode(ctx context.Context, target any, args ...string) error {
	body, err := c.run(ctx, nil, args...)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func boundedContainerOutput(cmd *exec.Cmd, input io.Reader) ([]byte, error) {
	var output bytes.Buffer
	var diagnostics bytes.Buffer
	cmd.Stdin = input
	cmd.Stdout = &containerOutput{destination: &output, remaining: 1 << 20}
	cmd.Stderr = &containerOutput{destination: &diagnostics, remaining: 64 << 10}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	return output.Bytes(), nil
}

type containerOutput struct {
	destination io.Writer
	remaining   int
}

func (w *containerOutput) Write(body []byte) (int, error) {
	if len(body) > w.remaining {
		return 0, errors.New("container response exceeds observation limit")
	}
	w.remaining -= len(body)
	return w.destination.Write(body)
}
