//go:build linux

// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"github.com/albertize/grillo/internal/model"
	"github.com/albertize/grillo/internal/source"
)

// RuntimeCapabilities is the native implementation policy, not a host
// feasibility check. Offline planning uses the bridged production profile;
// the executor uses its actual configuration. Unknown features stay unavailable.
func RuntimeCapabilities(bridged bool) model.Capabilities {
	caps := model.NewCapabilities()
	for _, feature := range []model.Feature{
		model.FeatureBindMount, model.FeatureManagedVolume, model.FeaturePVCMount,
		model.FeatureInitContainers, model.FeatureMultipleContainers,
		model.FeatureExecProbe, model.FeatureHTTPProbe, model.FeatureTCPProbe,
		model.FeatureReadOnlyRootFilesystem,
	} {
		caps.Set(feature, source.Supported)
	}
	if bridged {
		caps.Set(model.FeatureHostPort, source.Supported)
		caps.Set(model.FeatureRoute, source.Supported)
	}
	return caps
}

// ValidateApplication combines structural/capability checks and implementation
// restrictions before secrets, desired state or runtime resources are changed.
func ValidateApplication(app model.Application, bridged bool) source.List {
	diagnostics := model.Validate(app, RuntimeCapabilities(bridged))
	if !diagnostics.HasErrors() {
		if err := ValidateDesired(app); err != nil {
			diagnostics = append(diagnostics, source.Diagnostic{Severity: source.SeverityError, Compatibility: source.Unsupported, Code: "runtime.unsupported_semantics", Resource: "application/" + app.Identity.Name, Message: err.Error(), Consequence: "the native runtime cannot faithfully execute this application", Remediation: "remove the unsupported runtime setting"})
		}
	}
	diagnostics.Sort()
	return diagnostics
}

func (e *Executor) ValidateApplication(app model.Application) source.List {
	return ValidateApplication(app, e.cfg.EnableNetwork)
}
