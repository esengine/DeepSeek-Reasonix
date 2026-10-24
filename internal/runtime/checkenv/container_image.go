package checkenv

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

type containerInspection struct {
	ID     string `json:"Id"`
	Image  string
	Config struct {
		Env                               []string
		Labels                            map[string]string
		OpenStdin, StdinOnce, AttachStdin bool
	}
	State struct {
		Running       bool
		Pid, ExitCode int
		Error         string
	}
	HostConfig json.RawMessage
	Mounts     json.RawMessage
	rawConfig  json.RawMessage
}

func (c *containerClient) inspect(ctx context.Context, id string) (containerInspection, error) {
	body, err := c.run(ctx, nil, "container", "inspect", id)
	if err != nil {
		return containerInspection{}, err
	}
	var objects []containerInspection
	err = json.Unmarshal(body, &objects)
	if err != nil || len(objects) != 1 {
		return containerInspection{}, errors.Join(err, errors.New("incomplete container observation"))
	}
	var complete []struct{ Config json.RawMessage }
	if json.Unmarshal(body, &complete) != nil || len(complete) != 1 {
		return containerInspection{}, errors.New("incomplete container configuration")
	}
	objects[0].rawConfig = complete[0].Config
	return objects[0], nil
}

func (object containerInspection) configuration() string {
	var host map[string]any
	decoder := json.NewDecoder(bytes.NewReader(object.HostConfig))
	decoder.UseNumber()
	if decoder.Decode(&host) != nil {
		return ""
	}
	// Docker changes the deprecated false OomKillDisable to null on start;
	// both leave the ordinary OOM killer enabled.
	if host["OomKillDisable"] == nil || host["OomKillDisable"] == false {
		delete(host, "OomKillDisable")
	}
	return digest(struct {
		Config, Mounts json.RawMessage
		Host           map[string]any
	}{object.rawConfig, object.Mounts, host})
}

func (i *containerEnvironment) prepareImage(ctx context.Context, image string, request tool.ControlledShellRequest) error {
	var images []struct {
		ID, OS, Architecture string
		Config               struct {
			Env     []string
			Volumes map[string]json.RawMessage
		}
	}
	if err := i.client.decode(ctx, &images, "image", "inspect", image); err != nil {
		return err
	}
	if len(images) != 1 || images[0].ID != image || images[0].OS != "linux" ||
		len(images[0].Config.Volumes) != 0 || containerArchitecture(images[0].Architecture) != containerArchitecture(i.client.engine.Architecture) {
		return ErrUnsupported
	}
	i.journal = containerJournal{Version: 1, Identity: i.identity, Engine: i.client.engine.ID, Endpoint: i.client.endpoint,
		Stage: i.identity + "-stage", Run: i.identity + "-run"}
	if err := i.saveJournal(); err != nil {
		return err
	}
	if _, err := i.client.run(ctx, nil, "create", "--pull=never", "--name", i.journal.Stage,
		"--label", containerOwnerLabel+"="+i.identity, "--entrypoint", "/bin/bash", image); err != nil {
		return err
	}
	interpreter, err := i.client.interpreter(ctx, i.journal.Stage)
	if err != nil {
		return err
	}
	i.input.Interpreter, i.input.Executable = interpreter, i.client.version
	archive, err := i.sourceArchive(ctx)
	if err != nil {
		return err
	}
	defer os.Remove(archive)
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	timeout := transferTimeout(info.Size())
	_, copyErr := i.client.runWithin(ctx, timeout, file, "cp", "-", i.journal.Stage+":/")
	err = errors.Join(copyErr, file.Close())
	if err != nil {
		return err
	}
	body, err := i.client.runWithin(ctx, timeout, nil, "commit", "--change", "LABEL "+containerOwnerLabel+"="+i.identity, i.journal.Stage)
	if err != nil {
		return err
	}
	i.journal.Image, err = containerImageID("docker-image://" + strings.TrimSpace(string(body)))
	if err != nil {
		return fmt.Errorf("unrecognized committed image %q: %w", string(body), err)
	}
	if err := i.saveJournal(); err != nil {
		return err
	}
	args, err := i.runCreateArguments(request, images[0].Config.Env)
	if err != nil {
		return err
	}
	if _, err := i.client.run(ctx, nil, args...); err != nil {
		return err
	}
	object, err := i.client.inspect(ctx, i.journal.Run)
	if err != nil {
		return err
	}
	if object.Image != i.journal.Image || object.Config.Labels[containerOwnerLabel] != i.identity ||
		!object.Config.OpenStdin || !object.Config.StdinOnce || !object.Config.AttachStdin {
		return errors.New("container liveness contract was not enforced")
	}
	if err := i.verifyUploadedSource(ctx); err != nil {
		return err
	}
	i.journal.RunID = object.ID
	i.journal.Configuration = object.configuration()
	return i.saveJournal()
}

func (i *containerEnvironment) runCreateArguments(request tool.ControlledShellRequest, imageEnvironment []string) ([]string, error) {
	payload, err := tool.BashVerificationPayload(i.command, request.PipeStatusMarker)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(i.spec.SourceRoot, i.spec.Directory)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, ErrUnsupported
	}
	directory := path.Join(i.sourcePath, filepath.ToSlash(relative))
	args := []string{"create", "--pull=never", "--name", i.journal.Run, "--label", containerOwnerLabel + "=" + i.identity,
		"--read-only", "--network=none", "--ipc=private", "--cgroupns=private", "--user=0:0", "--workdir=/", "--hostname=reasonix-check",
		"--cap-drop=ALL", "--cap-add=SETUID", "--cap-add=SETGID", "--cap-add=SETPCAP", "--security-opt=no-new-privileges=true",
		"--no-healthcheck", "--restart=no", "--pids-limit=256", "--interactive", "--attach=stdin", "--attach=stdout", "--attach=stderr",
		"--tmpfs=/tmp:rw,exec,nosuid,nodev,mode=1777", "--tmpfs=/run/reasonix-control:rw,nosuid,nodev,noexec,size=64k,mode=0700",
		"--entrypoint=/bin/bash"}
	for _, entry := range imageEnvironment {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !containerEnvironmentKey(key) {
			return nil, errors.New("invalid verification image environment")
		}
		args = append(args, "--env", key)
	}
	args = append(args, i.journal.Image, "--noprofile", "--norc", "-c", containerSupervisor, "--", directory, payload)
	declared := map[string]string{"HOME": "/tmp", "TMPDIR": "/tmp", "PATH": "/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "LANG": "C", "LC_ALL": "C"}
	for key, value := range request.DeclaredEnvironment {
		if !containerEnvironmentKey(key) || strings.ContainsRune(value, 0) {
			return nil, errors.New("invalid declared verification environment")
		}
		declared[key] = value
	}
	for _, key := range slices.Sorted(maps.Keys(declared)) {
		args = append(args, key+"="+declared[key])
	}
	return args, nil
}

func containerEnvironmentKey(key string) bool {
	if key == "" {
		return false
	}
	for index, r := range key {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (index == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

const containerSupervisor = `set +e
umask 077
exec 9<&0
(/bin/bash --noprofile --norc -c 'IFS= read -r ignored <&9; exit 0') &
watcher=$!
directory=$1
payload=$2
shift 2
/usr/bin/setpriv --reuid=65534 --regid=65534 --clear-groups --bounding-set=-all --inh-caps=-all --ambient-caps=-all --no-new-privs /usr/bin/env -i "$@" /bin/bash --noprofile --norc -c 'cd -- "$1" || exit 124; exec /bin/bash --noprofile --norc -c "$2"' -- "$directory" "$payload" </dev/null 9<&- &
child=$!
wait -n -p finished "$child" "$watcher"
status=$?
[[ $finished == "$child" ]] || exit 125
printf '%s\n' "$status" > /run/reasonix-control/status || exit 125
wait "$watcher"
exit "$status"`

func (c *containerClient) interpreter(ctx context.Context, container string) (fileutil.ContentVersion, error) {
	var output bytes.Buffer
	cmd := c.command(ctx, "cp", "--follow-link", container+":/bin/bash", "-")
	cmd.Stdout, cmd.Stderr = &containerOutput{destination: &output, remaining: 32 << 20}, io.Discard
	if err := cmd.Run(); err != nil {
		return fileutil.ContentVersion{}, err
	}
	reader := tar.NewReader(&output)
	header, err := reader.Next()
	if err != nil || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Mode&0111 == 0 {
		return fileutil.ContentVersion{}, errors.Join(ErrUnsupported, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return fileutil.ContentVersion{}, err
	}
	if _, err := reader.Next(); !errors.Is(err, io.EOF) {
		return fileutil.ContentVersion{}, ErrUnsupported
	}
	return fileutil.ContentVersion{Kind: "file", Digest: hex.EncodeToString(hash.Sum(nil)), Mode: uint32(header.Mode & 0777)}, nil
}
