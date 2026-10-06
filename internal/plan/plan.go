// SPDX-License-Identifier: Apache-2.0

// Package plan turns a desired application IR and an observed runtime state into
// an ordered, typed action list. It is pure: it computes no effects and imports
// no VMM, network, or process code.
package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/albertize/grillo/internal/model"
)

// ActionKind is a typed plan action.
type ActionKind string

const (
	ActionPullImage       ActionKind = "PullImage"
	ActionPrepareVolume   ActionKind = "PrepareVolume"
	ActionCreateSandbox   ActionKind = "CreateSandbox"
	ActionUpdateEndpoints ActionKind = "UpdateEndpoints"
	ActionShutdownNetwork ActionKind = "ShutdownNetwork"
	ActionDrain           ActionKind = "Drain"
	ActionStopSandbox     ActionKind = "StopSandbox"
	ActionDeleteSandbox   ActionKind = "DeleteSandbox"
	ActionDeleteVolume    ActionKind = "DeleteVolume"
)

// DataImpact describes whether an action can destroy data.
type DataImpact string

const (
	ImpactNone       DataImpact = "none"
	ImpactEphemeral  DataImpact = "ephemeral"
	ImpactPersistent DataImpact = "persistent"
)

// Descriptor identifies one desired sandbox.
type Descriptor struct {
	ID           string
	Workload     string
	Index        int
	Ordinal      bool
	TemplateHash string
}

// Action is one step of a plan.
type Action struct {
	Kind     ActionKind
	Resource string
	Reason   string
	Impact   DataImpact
	Image    string
	Volume   string
	Sandbox  Descriptor
}

// Plan is an ordered action list for one application.
type Plan struct {
	Application string
	Actions     []Action
}

// Empty reports whether the plan performs no actions (an identical apply).
func (p Plan) Empty() bool { return len(p.Actions) == 0 }

// Reboots counts actions that stop or replace a sandbox, so tests can assert that
// a route-only change reboots nothing.
func (p Plan) Reboots() int {
	count := 0
	for _, action := range p.Actions {
		switch action.Kind {
		case ActionCreateSandbox, ActionStopSandbox, ActionDeleteSandbox:
			count++
		}
	}
	return count
}

// ObservedSandbox is the last known runtime state of one sandbox.
type ObservedSandbox struct {
	Workload     string
	Index        int
	TemplateHash string
	State        string
	Ready        bool
}

// Observed is the last known runtime state for one application.
type Observed struct {
	Revision       string
	RoutesHash     string
	Stopped        bool
	NetworkPending bool // cleanup intent persisted before a possible network effect
	Sandboxes      map[string]ObservedSandbox
	Volumes        map[string]bool
}

// DesiredSandboxes returns the sandboxes a desired application requires.
func DesiredSandboxes(app model.Application) ([]Descriptor, error) {
	var descriptors []Descriptor
	for _, workload := range app.Workloads {
		hash, err := templateHash(app, workload)
		if err != nil {
			return nil, err
		}
		replicas := int(workload.Replicas)
		if replicas < 0 {
			replicas = 0
		}
		for index := 0; index < replicas; index++ {
			descriptors = append(descriptors, Descriptor{
				ID:           fmt.Sprintf("%s-%d", workload.ID, index),
				Workload:     workload.ID,
				Index:        index,
				Ordinal:      workload.Kind == model.WorkloadStatefulSet || workload.Kind == model.WorkloadJob,
				TemplateHash: hash,
			})
		}
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].ID < descriptors[j].ID })
	return descriptors, nil
}

// Build diffs desired against observed and returns an ordered action list.
func Build(desired model.Application, observed Observed) (Plan, error) {
	name := desired.Identity.Name
	result := Plan{Application: name}
	revision, err := model.Hash(desired)
	if err != nil {
		return Plan{}, err
	}
	routesHash, err := hashRoutes(desired)
	if err != nil {
		return Plan{}, err
	}

	if observed.Stopped {
		// `down`: remove everything observed, never create.
		ids := sortedIDs(observed.Sandboxes)
		for _, id := range ids {
			descriptor := ObservedDescriptor(id, observed.Sandboxes[id])
			result.Actions = append(result.Actions, stopActions(descriptor)...)
		}
		if observed.NetworkPending || observed.RoutesHash != "" {
			result.Actions = append(result.Actions, Action{Kind: ActionShutdownNetwork, Resource: "application/" + name, Reason: "application is stopped", Impact: ImpactNone})
		}
		return result, nil
	}

	descriptors, err := DesiredSandboxes(desired)
	if err != nil {
		return Plan{}, err
	}
	desiredByID := make(map[string]Descriptor, len(descriptors))
	for _, descriptor := range descriptors {
		desiredByID[descriptor.ID] = descriptor
	}

	// Volumes before sandboxes.
	for _, volume := range desired.Volumes {
		if volume.Kind == model.VolumeEphemeral {
			continue
		}
		if observed.Volumes != nil && observed.Volumes[volume.Name] {
			continue
		}
		result.Actions = append(result.Actions, Action{
			Kind:     ActionPrepareVolume,
			Resource: "volume/" + volume.Name,
			Reason:   "volume is not prepared",
			Impact:   ImpactNone,
			Volume:   volume.Name,
		})
	}

	for _, descriptor := range descriptors {
		previous, exists := observed.Sandboxes[descriptor.ID]
		switch {
		case !exists:
			result.Actions = append(result.Actions, createAction(descriptor, "sandbox does not exist"))
		case previous.TemplateHash != descriptor.TemplateHash:
			result.Actions = append(result.Actions, stopActions(descriptor)...)
			result.Actions = append(result.Actions, createAction(descriptor, "template changed"))
		case previous.State == "stopped" || previous.State == "pending" || previous.State == "failed":
			if shouldRestart(desired, descriptor.Workload, previous.State) {
				result.Actions = append(result.Actions, createAction(descriptor, "sandbox is not running"))
			}
		}
	}

	for _, id := range sortedIDs(observed.Sandboxes) {
		if _, ok := desiredByID[id]; ok {
			continue
		}
		descriptor := ObservedDescriptor(id, observed.Sandboxes[id])
		result.Actions = append(result.Actions, stopActions(descriptor)...)
	}

	if observed.RoutesHash != routesHash {
		result.Actions = append(result.Actions, Action{
			Kind:     ActionUpdateEndpoints,
			Resource: "application/" + name,
			Reason:   "routes or services changed",
			Impact:   ImpactNone,
		})
	}

	if observed.Revision == revision && !result.Empty() {
		// The revision matches but the sandbox set does not: this is drift, not a
		// new desired revision. Keep the actions so drift is corrected.
		_ = revision
	}
	return result, nil
}

// ObservedDescriptor reconstructs a descriptor for a sandbox that is no longer
// desired, so it can be stopped and deleted.
func ObservedDescriptor(id string, observed ObservedSandbox) Descriptor {
	return Descriptor{ID: id, Workload: observed.Workload, Index: observed.Index, TemplateHash: observed.TemplateHash}
}

func createAction(descriptor Descriptor, reason string) Action {
	return Action{
		Kind:     ActionCreateSandbox,
		Resource: "sandbox/" + descriptor.ID,
		Reason:   reason,
		Impact:   ImpactEphemeral,
		Sandbox:  descriptor,
	}
}

func stopActions(descriptor Descriptor) []Action {
	return []Action{
		{Kind: ActionDrain, Resource: "sandbox/" + descriptor.ID, Reason: "removing from endpoints", Impact: ImpactNone, Sandbox: descriptor},
		{Kind: ActionStopSandbox, Resource: "sandbox/" + descriptor.ID, Reason: "stopping sandbox", Impact: ImpactEphemeral, Sandbox: descriptor},
		{Kind: ActionDeleteSandbox, Resource: "sandbox/" + descriptor.ID, Reason: "deleting sandbox", Impact: ImpactEphemeral, Sandbox: descriptor},
	}
}

func sortedIDs(sandboxes map[string]ObservedSandbox) []string {
	ids := make([]string, 0, len(sandboxes))
	for id := range sandboxes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Revision returns the canonical hash of a desired application.
func Revision(app model.Application) (string, error) { return model.Hash(app) }

// RoutesHash returns a hash of only the services and routes, so a route-only
// change can be detected without rebooting sandboxes.
func RoutesHash(app model.Application) (string, error) { return hashRoutes(app) }

func shouldRestart(app model.Application, workloadID, state string) bool {
	for i := range app.Workloads {
		workload := &app.Workloads[i]
		if workload.ID != workloadID {
			continue
		}
		switch workload.RestartPolicy {
		case model.RestartNever:
			// A completed Job must not restart forever.
			return false
		case model.RestartOnFailure:
			return state == "failed"
		default:
			return true
		}
	}
	return true
}

func hashRoutes(app model.Application) (string, error) {
	input := struct {
		Services []model.Service
		Routes   []model.Route
	}{Services: app.Services, Routes: app.Routes}
	return hashValue(input)
}

// templateHash captures everything that should force sandbox replacement: the
// template plus the volumes, configs, and secret versions it references.
func templateHash(app model.Application, workload model.Workload) (string, error) {
	volumes := map[string]bool{}
	for _, name := range workload.Template.Volumes {
		volumes[name] = true
	}
	configs := map[string]bool{}
	secrets := map[string]bool{}
	containers := append(append([]model.Container{}, workload.Template.InitContainers...), workload.Template.Containers...)
	for _, container := range containers {
		for _, mount := range container.Mounts {
			volumes[mount.Volume] = true
		}
		for _, env := range container.Env {
			if env.ValueFrom == nil {
				continue
			}
			if env.ValueFrom.ConfigRef != nil {
				configs[env.ValueFrom.ConfigRef.Config] = true
			}
			if env.ValueFrom.SecretRef != nil {
				secrets[env.ValueFrom.SecretRef.Secret] = true
			}
		}
	}
	input := struct {
		Template model.SandboxTemplate
		Volumes  []model.Volume
		Configs  []model.Config
		Secrets  []model.SecretRef
	}{Template: workload.Template}
	for _, volume := range app.Volumes {
		if volumes[volume.Name] {
			input.Volumes = append(input.Volumes, volume)
		}
	}
	for _, config := range app.Configs {
		if configs[config.Name] {
			input.Configs = append(input.Configs, config)
		}
	}
	for _, secret := range app.Secrets {
		if secrets[secret.Name] {
			input.Secrets = append(input.Secrets, secret)
		}
	}
	sort.Slice(input.Volumes, func(i, j int) bool { return input.Volumes[i].Name < input.Volumes[j].Name })
	sort.Slice(input.Configs, func(i, j int) bool { return input.Configs[i].Name < input.Configs[j].Name })
	sort.Slice(input.Secrets, func(i, j int) bool { return input.Secrets[i].Name < input.Secrets[j].Name })
	return hashValue(input)
}

func hashValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
