// SPDX-License-Identifier: Apache-2.0

package source

// RegisteredFeature is a public, value-only snapshot of a frontend's validator
// registry. State classifies compilation, never runtime acceptance. Fixtures
// identify regression suites; they are not per-field hardware evidence.
type RegisteredFeature struct {
	Format      string        `json:"format"`
	Version     string        `json:"version"`
	Scope       string        `json:"scope"`
	Field       string        `json:"field"`
	Milestone   string        `json:"milestone"`
	State       Compatibility `json:"state"`
	Default     string        `json:"default"`
	Consequence string        `json:"consequence"`
	Fixtures    []string      `json:"fixtures"`
}
