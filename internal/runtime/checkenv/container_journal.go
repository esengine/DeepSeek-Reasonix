package checkenv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

const containerOwnerLabel = "org.reasonix.controlled-check"

type containerJournal struct {
	Version       int    `json:"version"`
	Identity      string `json:"identity"`
	Engine        string `json:"engine"`
	Endpoint      string `json:"endpoint"`
	Stage         string `json:"stage"`
	Run           string `json:"run"`
	RunID         string `json:"run_id,omitempty"`
	Image         string `json:"image,omitempty"`
	Configuration string `json:"configuration,omitempty"`
}

func (i *containerEnvironment) saveJournal() error {
	raw, err := json.Marshal(i.journal)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(filepath.Join(i.directory, "container.json"), raw, 0600)
}

func recoverContainerJournals(ctx context.Context, storage, current string, client *containerClient) error {
	entries, err := os.ReadDir(storage)
	if err != nil {
		return err
	}
	if len(entries) > 1000 {
		return errors.New("verification recovery directory exceeds observation limit")
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "check-") {
			continue
		}
		directory := filepath.Join(storage, entry.Name())
		if directory == current {
			continue
		}
		if err := recoverContainerJournal(ctx, directory, client); err != nil {
			if ctx.Err() != nil {
				return err
			}
			slog.Warn("verification recovery journal retained", "directory", directory, "err", err)
		}
	}
	return nil
}

func recoverContainerJournal(ctx context.Context, directory string, client *containerClient) error {
	release, err := filelock.TryAcquire(filepath.Join(directory, "owner.lock"))
	if errors.Is(err, filelock.ErrHeld) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	raw, err := os.ReadFile(filepath.Join(directory, "container.json"))
	if os.IsNotExist(err) {
		// Removed under the lock: an owner that has not locked it yet then fails
		// instead of writing into a directory being deleted.
		removeFrozenTree(directory)
		return nil
	}
	if err != nil {
		return err
	}
	var journal containerJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 1<<20 || decoder.Decode(&journal) != nil || journal.Version != 1 || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return errors.New("unsupported verification recovery journal; original retained")
	}
	if client == nil || journal.Engine != client.engine.ID || journal.Endpoint != client.endpoint {
		return nil
	}
	if journal.Identity != filepath.Base(directory) || journal.Stage != journal.Identity+"-stage" || journal.Run != journal.Identity+"-run" {
		return errors.New("verification recovery identity changed; original retained")
	}
	if err := journal.clean(ctx, client); err != nil {
		return err
	}
	release()
	removeFrozenTree(directory)
	return nil
}

func (j containerJournal) clean(ctx context.Context, client *containerClient) error {
	for _, name := range []string{j.Run, j.Stage} {
		body, listErr := client.run(ctx, nil, "container", "ls", "--all", "--filter", "name=^/"+name+"$", "--format", "{{.ID}}")
		if listErr != nil {
			return listErr
		}
		if strings.TrimSpace(string(body)) == "" {
			continue
		}
		object, err := client.inspect(ctx, name)
		if err != nil {
			return err
		}
		if object.Config.Labels[containerOwnerLabel] != j.Identity {
			return errors.New("verification cleanup ownership mismatch")
		}
		if _, err := client.run(ctx, nil, "container", "rm", "--force", object.ID); err != nil {
			return err
		}
	}
	body, err := client.run(ctx, nil, "image", "ls", "--all", "--quiet", "--no-trunc", "--filter", "label="+containerOwnerLabel+"="+j.Identity)
	if err != nil {
		return err
	}
	for id := range strings.FieldsSeq(string(body)) {
		if !strings.HasPrefix(id, "sha256:") {
			return errors.New("invalid verification image identity")
		}
		if _, err := client.run(ctx, nil, "image", "rm", id); err != nil {
			return err
		}
	}
	return nil
}

func (i *containerEnvironment) cleanup() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.directory == "" {
		return
	}
	if i.controlWrite != nil {
		_ = i.controlWrite.Close()
	}
	if i.controlRead != nil {
		_ = i.controlRead.Close()
	}
	if i.client != nil && i.journal.Version != 0 {
		if err := i.journal.clean(context.Background(), i.client); err != nil {
			return
		}
	}
	if i.release != nil {
		i.release()
		i.release = nil
	}
	removeFrozenTree(i.directory)
	i.directory = ""
}
