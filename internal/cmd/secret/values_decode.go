package secret

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

const maxVaultValuesBytes = 512 * 1024

// decodeVaultValues token-walks to reject duplicate keys before a map can
// overwrite them. Errors never contain the input, including decoder errors.
func decodeVaultValues(input io.Reader) (map[string]string, error) {
	raw, err := io.ReadAll(io.LimitReader(input, maxVaultValuesBytes+1))
	if err != nil {
		return nil, cli.MisuseError("could not read values input")
	}
	if len(raw) > maxVaultValuesBytes {
		return nil, cli.MisuseError("values input exceeds the 512 KiB limit")
	}
	invalid := func() (map[string]string, error) {
		return nil, cli.MisuseError("values must be a non-empty JSON object with unique valid keys and string values")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return invalid()
	}
	values := map[string]string{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return invalid()
		}
		key, ok := token.(string)
		if !ok || len(validation.IsConfigMapKey(key)) > 0 {
			return invalid()
		}
		if _, exists := values[key]; exists {
			return invalid()
		}
		token, err = decoder.Token()
		if err != nil {
			return invalid()
		}
		value, ok := token.(string)
		if !ok {
			return invalid()
		}
		values[key] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || len(values) == 0 {
		return invalid()
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return invalid()
	}
	return values, nil
}
