package config

import (
	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func decodeTOMLFile(path string, v any) (toml.MetaData, error) {
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	return toml.Decode(string(data), v)
}
