// SPDX-License-Identifier: Apache-2.0

package model

import "strings"

// Redacted is the marker substituted for sensitive values in public output.
const Redacted = "[redacted]"

// Public returns a deep copy of an application safe to show in plans, inspect
// output, events, the UI, and logs. The IR stores secret references, never
// secret values; any Config entry explicitly marked Sensitive is masked.
func Public(app Application) (Application, error) {
	out, err := cloneApplication(app)
	if err != nil {
		return Application{}, err
	}
	for i := range out.Configs {
		for j := range out.Configs[i].Entries {
			entry := &out.Configs[i].Entries[j]
			if entry.Sensitive {
				entry.Text = Redacted
				entry.Binary = nil
			}
		}
	}
	return out, nil
}

// RedactText removes known secret values from arbitrary text such as a wrapped
// error or a tool's stderr before it is logged or shown publicly.
func RedactText(text string, secretValues []string) string {
	for _, value := range secretValues {
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, Redacted)
	}
	return text
}
