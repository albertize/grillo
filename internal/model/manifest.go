// SPDX-License-Identifier: Apache-2.0

package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"grillo.local/grillo/internal/source"
)

// CodeManifestDecode reports a malformed native manifest.
const CodeManifestDecode = "ir.manifest.decode"

// LoadManifest decodes the experimental native JSON manifest used by runtime and
// frontend tests. It is not a stable public format: unknown fields are rejected
// so typos fail loudly, and the result is normalized before it is returned.
func LoadManifest(data []byte) (Application, source.List, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var app Application
	if err := dec.Decode(&app); err != nil {
		return Application{}, source.List{{Code: CodeManifestDecode, Severity: source.SeverityError, Message: err.Error()}}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Application{}, source.List{{Code: CodeManifestDecode, Severity: source.SeverityError, Message: "trailing data after manifest"}}, errors.New("decode manifest: trailing data")
	}
	Normalize(&app)
	return app, nil, nil
}

// MarshalManifest renders an application as the native JSON manifest. Output is
// deterministic because the application is normalized first.
func MarshalManifest(app Application) ([]byte, error) {
	clone, err := cloneApplication(app)
	if err != nil {
		return nil, err
	}
	Normalize(&clone)
	data, err := json.MarshalIndent(clone, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
