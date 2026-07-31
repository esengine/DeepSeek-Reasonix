package config

import (
	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func decodeTOMLFile(path string, v any) (toml.MetaData, error) {
	resolved, err := resolveConfigReadPath(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	return decodeTOMLFileResolved(resolved, v)
}

func decodeTOMLFileResolved(path string, v any) (toml.MetaData, error) {
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return toml.MetaData{}, err
	}
	return decodeTOMLBytes(data, v)
}

func decodeTOMLBytes(data []byte, v any) (toml.MetaData, error) {
	return toml.Decode(string(fileencoding.DecodeToUTF8(data)), v)
}
