package sessioninbox

import (
	"encoding/json"
	"os"
	"path/filepath"

	"reasonix/internal/attachment"
	"reasonix/internal/sessioncontent"
)

// FrozenContentRefs reads only manifest-reachable blobs. The caller owns the
// disk transaction lock or an exclusive copied snapshot; no state is created.
func FrozenContentRefs(dir string) ([]sessioncontent.Ref, error) {
	data, err := readRegularFile(filepath.Join(dir, manifestName), maxManifestBytes)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	man, err := decodeManifest(data)
	if err != nil {
		return nil, err
	}
	if man.SchemaVersion > SchemaVersion {
		return nil, ErrSchemaReadonly
	}
	reader := &Store{dir: dir, limits: (Limits{}).withDefaults()}
	var refs []sessioncontent.Ref
	for _, item := range man.Items {
		env, err := reader.readBlobLocked(blobNameFor(item), item.Checksum)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(env)
		if err != nil {
			return nil, err
		}
		refs = append(refs, attachment.CollectJSONRefs(body)...)
	}
	return refs, nil
}
