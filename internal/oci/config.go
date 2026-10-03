// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ImageConfig is the subset of an OCI/Docker image config Grillo consumes.
type ImageConfig struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Config       struct {
		User       string            `json:"User,omitempty"`
		Env        []string          `json:"Env,omitempty"`
		Entrypoint []string          `json:"Entrypoint,omitempty"`
		Cmd        []string          `json:"Cmd,omitempty"`
		WorkingDir string            `json:"WorkingDir,omitempty"`
		StopSignal string            `json:"StopSignal,omitempty"`
		Labels     map[string]string `json:"Labels,omitempty"`
	} `json:"config"`
	RootFS struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
	History []ConfigHistory `json:"history,omitempty"`
}

// ConfigHistory is one image-config history entry.
type ConfigHistory struct {
	Created    string `json:"created,omitempty"`
	CreatedBy  string `json:"created_by,omitempty"`
	Author     string `json:"author,omitempty"`
	Comment    string `json:"comment,omitempty"`
	EmptyLayer bool   `json:"empty_layer,omitempty"`
}

// ParseImageConfig decodes an image config blob.
func ParseImageConfig(data []byte) (ImageConfig, error) {
	var config ImageConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return ImageConfig{}, fmt.Errorf("oci: decode image config: %w", err)
	}
	for i, diffID := range config.RootFS.DiffIDs {
		if !digestRe.MatchString(diffID) {
			return ImageConfig{}, fmt.Errorf("oci: rootfs diff_id %d is invalid", i)
		}
	}
	return config, nil
}

// RuntimeOptions are caller overrides that win over the image config.
type RuntimeOptions struct {
	Entrypoint []string
	Cmd        []string
	Env        []string
	User       string
	WorkingDir string
}

// ResolvedProcess is the merged process configuration.
type ResolvedProcess struct {
	Args       []string
	Env        []string
	User       string
	WorkingDir string
	StopSignal string
}

// MergeConfig applies runtime overrides to the image config, preserving explicit
// overrides and merging environment by key.
func MergeConfig(image ImageConfig, opts RuntimeOptions) ResolvedProcess {
	entrypoint := image.Config.Entrypoint
	if len(opts.Entrypoint) > 0 {
		entrypoint = opts.Entrypoint
	}
	cmd := image.Config.Cmd
	if len(opts.Cmd) > 0 {
		cmd = opts.Cmd
	}
	user := image.Config.User
	if opts.User != "" {
		user = opts.User
	}
	workdir := image.Config.WorkingDir
	if opts.WorkingDir != "" {
		workdir = opts.WorkingDir
	}
	return ResolvedProcess{
		Args:       append(append([]string(nil), entrypoint...), cmd...),
		Env:        mergeEnv(image.Config.Env, opts.Env),
		User:       user,
		WorkingDir: workdir,
		StopSignal: image.Config.StopSignal,
	}
}

func mergeEnv(base, override []string) []string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	index := map[string]int{}
	out := make([]string, 0, len(base)+len(override))
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		index[key] = len(out)
		out = append(out, kv)
	}
	for _, kv := range override {
		key, _, _ := strings.Cut(kv, "=")
		if i, ok := index[key]; ok {
			out[i] = kv
			continue
		}
		index[key] = len(out)
		out = append(out, kv)
	}
	return out
}
