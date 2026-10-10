// SPDX-License-Identifier: Apache-2.0

package guest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// encoding/json otherwise accepts duplicate fields with last-value wins. Reject
// ambiguous inventories before decoding typed expectations. Depth is bounded
// independently of the byte limit, including unknown fields.
func rejectDuplicateManifestFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func(int) error
	value = func(depth int) error {
		if depth > 8 {
			return fmt.Errorf("guest: manifest JSON nesting exceeds limit")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("guest: duplicate or invalid manifest JSON field")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("guest: invalid manifest JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("guest: trailing manifest JSON data")
	}
	return nil
}
