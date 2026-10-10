// SPDX-License-Identifier: Apache-2.0

package build

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/albertize/grillo/internal/dockerfile"
	"github.com/albertize/grillo/internal/image"
	"github.com/albertize/grillo/internal/oci"
)

// NativeBuilder builds images from a supported Dockerfile subset without any
// external container engine. RUN steps execute through a Runner; a build with
// no RUN needs no sandbox at all.
type NativeBuilder struct {
	Store        *image.Store
	CAS          *oci.CAS
	Images       ImageResolver
	Runner       Runner
	ScratchDir   string
	Platform     oci.Platform
	MaxBlobBytes int64
	Now          func() time.Time
}

type stageState struct {
	rootfs     string
	baseLayers []oci.Descriptor
	diffIDs    []string
	config     oci.ImageConfig
	envSet     bool

	cmd        []string
	entrypoint []string
	env        []string
	workdir    string
	user       string
	labels     map[string]string
	exposed    map[string]struct{}
	owners     map[string]fileOwner
	history    []historyEntry
	snapshot   map[string]fileEntry
}

type historyEntry struct {
	Created    string
	CreatedBy  string
	EmptyLayer bool
}

type buildContext struct {
	request  Request
	parsed   *dockerfile.File
	ignore   *dockerfile.Ignore
	args     map[string]string
	roots    map[string]*stageState
	scratch  string
	progress Progress
	now      time.Time
}

// Build implements Builder.
func (b *NativeBuilder) Build(ctx context.Context, request Request, progress Progress) (Result, error) {
	if b.CAS == nil || b.Store == nil {
		return Result{}, errors.New("build: native builder requires a CAS and an image store")
	}
	if b.Images == nil {
		return Result{}, errors.New("build: native builder requires an image resolver")
	}
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	dockerfilePath := request.Dockerfile
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}
	if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(request.ContextDir, dockerfilePath)
	}
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return Result{}, fmt.Errorf("build: read %s: %w", dockerfilePath, err)
	}
	parsed, err := dockerfile.Parse(data)
	if err != nil {
		return Result{}, err
	}
	if len(parsed.Stages) == 0 {
		return Result{}, errors.New("build: Dockerfile has no FROM instruction")
	}
	if err := checkSupported(parsed); err != nil {
		return Result{}, err
	}

	started := time.Now()
	now := time.Now().UTC()
	if b.Now != nil {
		now = b.Now().UTC()
	}
	scratch, err := os.MkdirTemp(b.ScratchDir, "grillo-native-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(scratch)
	if b.Runner != nil {
		defer b.Runner.Close()
	}

	args := map[string]string{}
	for key, value := range parsed.GlobalArgs {
		args[key] = value
	}
	for key, value := range request.BuildArgs {
		args[key] = value
	}
	build := &buildContext{
		request:  request,
		parsed:   parsed,
		ignore:   loadIgnore(request.ContextDir),
		args:     args,
		roots:    map[string]*stageState{},
		scratch:  scratch,
		progress: progress,
		now:      now,
	}

	prior, hadPrior, err := b.Store.Get(request.Reference)
	if err != nil {
		return Result{}, err
	}

	var final *stageState
	for i := range parsed.Stages {
		state, err := b.buildStage(ctx, build, parsed.Stages[i], i)
		if err != nil {
			return Result{}, fmt.Errorf("build: stage %d: %w", i, err)
		}
		// Register by index always, and by name when the stage is named, so
		// both `COPY --from=0` and `COPY --from=name` resolve.
		build.roots[fmt.Sprintf("%d", i)] = state
		if name := parsed.Stages[i].Name; name != "" {
			build.roots[name] = state
		}
		final = state
	}

	layers := append([]oci.Descriptor{}, final.baseLayers...)
	diffIDs := append([]string{}, final.diffIDs...)
	configData, err := marshalConfig(b.platform(), final, diffIDs, now)
	if err != nil {
		return Result{}, err
	}
	configDigest := digestOf(configData)
	if err := b.CAS.PutBytes(configDigest, configData); err != nil {
		return Result{}, err
	}
	manifest := oci.Manifest{
		SchemaVersion: 2,
		MediaType:     oci.MediaTypeOCIManifest,
		Config: oci.Descriptor{
			MediaType: oci.MediaTypeOCIConfig,
			Digest:    configDigest,
			Size:      int64(len(configData)),
		},
		Layers: layers,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return Result{}, err
	}
	manifestDigest := digestOf(manifestData)
	if err := b.CAS.PutBytes(manifestDigest, manifestData); err != nil {
		return Result{}, err
	}
	parsedManifest, err := oci.ParseManifest(manifestData)
	if err != nil {
		return Result{}, err
	}
	parsedConfig, err := oci.ParseImageConfig(configData)
	if err != nil {
		return Result{}, err
	}
	pulled := oci.PulledImage{ManifestDigest: manifestDigest, Manifest: parsedManifest, Config: parsedConfig}
	record, err := b.Store.Import(pulled, image.Meta{
		Reference: request.Reference,
		Source:    image.SourceBuild,
		Labels:    request.Labels,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Reference:      request.Reference,
		ManifestDigest: manifestDigest,
		Record:         record,
		Image:          pulled,
		FromCache:      hadPrior && prior.ManifestDigest == manifestDigest,
		Duration:       time.Since(started),
	}, nil
}

func (b *NativeBuilder) platform() oci.Platform {
	if b.Platform.OS != "" {
		return b.Platform
	}
	return oci.Platform{OS: "linux", Architecture: "amd64"}
}

func (b *NativeBuilder) buildStage(ctx context.Context, build *buildContext, stage dockerfile.Stage, index int) (*stageState, error) {
	stageDir := filepath.Join(build.scratch, fmt.Sprintf("stage-%d", index))
	rootfs := filepath.Join(stageDir, "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		return nil, err
	}
	state, err := b.resolveStageBase(ctx, build, stage, rootfs)
	if err != nil {
		return nil, err
	}
	if !state.envSet {
		state.env = buildEnvOf(state.config)
		state.envSet = true
	}
	state.snapshot, err = snapshotTree(rootfs)
	if err != nil {
		return nil, err
	}
	for _, instruction := range stage.Instructions {
		empty, err := b.applyInstruction(ctx, build, state, instruction)
		if err != nil {
			return nil, err
		}
		state.history = append(state.history, historyEntry{
			CreatedBy:  strings.TrimSpace(instruction.Name + " " + instruction.Args),
			EmptyLayer: empty,
		})
	}
	// Commit this stage's changes so a later FROM can reuse its layers.
	layer, diffID, err := b.commitLayer(build.scratch, fmt.Sprintf("stage-%d", index), state.snapshot, state.rootfs, state.owners)
	if err != nil {
		return nil, err
	}
	if layer != nil {
		state.baseLayers = append(state.baseLayers, *layer)
		state.diffIDs = append(state.diffIDs, diffID)
	}
	return state, nil
}

func (b *NativeBuilder) resolveStageBase(ctx context.Context, build *buildContext, stage dockerfile.Stage, rootfs string) (*stageState, error) {
	state := &stageState{rootfs: rootfs, labels: map[string]string{}, exposed: map[string]struct{}{}, owners: map[string]fileOwner{}}
	switch {
	case stage.Base == "scratch":
		return state, nil
	}
	if source, ok := findStage(build, stage.Base); ok {
		if err := copyTree(source.rootfs, rootfs, copyOptions{}); err != nil {
			return nil, err
		}
		state.baseLayers = append([]oci.Descriptor{}, source.baseLayers...)
		state.diffIDs = append([]string{}, source.diffIDs...)
		state.config = source.config
		state.envSet = true
		state.cmd = append([]string{}, source.cmd...)
		state.entrypoint = append([]string{}, source.entrypoint...)
		state.env = append([]string{}, source.env...)
		state.workdir = source.workdir
		state.user = source.user
		state.history = append([]historyEntry{}, source.history...)
		for key, value := range source.labels {
			state.labels[key] = value
		}
		for key := range source.exposed {
			state.exposed[key] = struct{}{}
		}
		return state, nil
	}
	pulled, err := b.resolveImage(ctx, stage.Base, build.request.Pull)
	if err != nil {
		return nil, err
	}
	if err := b.Images.Unpack(pulled, rootfs, oci.UnpackOptions{Context: ctx}); err != nil {
		return nil, err
	}
	state.baseLayers = append([]oci.Descriptor{}, pulled.Manifest.Layers...)
	state.diffIDs = append([]string{}, pulled.Config.RootFS.DiffIDs...)
	state.config = pulled.Config
	state.cmd = append([]string{}, pulled.Config.Config.Cmd...)
	state.entrypoint = append([]string{}, pulled.Config.Config.Entrypoint...)
	state.workdir = pulled.Config.Config.WorkingDir
	state.user = pulled.Config.Config.User
	for key, value := range pulled.Config.Config.Labels {
		state.labels[key] = value
	}
	for _, entry := range pulled.Config.History {
		state.history = append(state.history, historyEntry{
			Created:    entry.Created,
			CreatedBy:  entry.CreatedBy,
			EmptyLayer: entry.EmptyLayer,
		})
	}
	return state, nil
}

func (b *NativeBuilder) resolveImage(ctx context.Context, reference string, pull bool) (oci.PulledImage, error) {
	if !pull {
		if record, ok, err := b.Store.Get(reference); err == nil && ok {
			if pulled, err := oci.LoadPulled(b.CAS, record.ManifestDigest); err == nil {
				return pulled, nil
			}
		}
	}
	ref, err := oci.ParseReference(reference)
	if err != nil {
		return oci.PulledImage{}, err
	}
	return b.Images.Pull(ctx, ref)
}

func (b *NativeBuilder) applyInstruction(ctx context.Context, build *buildContext, state *stageState, instruction dockerfile.Instruction) (bool, error) {
	switch instruction.Name {
	case "ARG":
		applyArg(instruction.Args, build.args)
		return true, nil
	case "ENV":
		pairs, err := parseKeyValues(instruction.Args)
		if err != nil {
			return false, err
		}
		for _, pair := range pairs {
			setEnv(&state.env, pair.key, expand(pair.value, build.envOf(state)))
		}
		return true, nil
	case "LABEL":
		pairs, err := parseKeyValues(instruction.Args)
		if err != nil {
			return false, err
		}
		for _, pair := range pairs {
			state.labels[pair.key] = expand(pair.value, build.envOf(state))
		}
		return true, nil
	case "WORKDIR":
		dir := expand(strings.TrimSpace(instruction.Args), build.envOf(state))
		if dir == "" {
			return false, errors.New("build: WORKDIR requires a path")
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(state.workdir, dir)
		}
		state.workdir = dir
		if err := os.MkdirAll(filepath.Join(state.rootfs, filepath.FromSlash(strings.TrimPrefix(dir, "/"))), 0o755); err != nil {
			return false, err
		}
		return true, nil
	case "USER":
		state.user = strings.TrimSpace(instruction.Args)
		return true, nil
	case "EXPOSE":
		for _, field := range strings.Fields(instruction.Args) {
			state.exposed[strings.ToUpper(field)] = struct{}{}
		}
		return true, nil
	case "CMD":
		cmd, err := instructionCommand(instruction)
		if err != nil {
			return false, err
		}
		state.cmd = cmd
		state.entrypoint = nil
		return true, nil
	case "ENTRYPOINT":
		entrypoint, err := instructionCommand(instruction)
		if err != nil {
			return false, err
		}
		state.entrypoint = entrypoint
		return true, nil
	case "COPY", "ADD":
		return false, b.applyCopy(ctx, build, state, instruction)
	case "RUN":
		return false, b.applyRun(ctx, build, state, instruction)
	case "HEALTHCHECK", "SHELL", "ONBUILD", "VOLUME", "STOPSIGNAL", "MAINTAINER":
		return false, fmt.Errorf("build: unsupported Dockerfile instruction %s (line %d)", instruction.Name, instruction.Line)
	default:
		return false, fmt.Errorf("build: unsupported instruction %s", instruction.Name)
	}
}

func (b *NativeBuilder) applyRun(ctx context.Context, build *buildContext, state *stageState, instruction dockerfile.Instruction) error {
	if b.Runner == nil {
		return errors.New("build: RUN requires a sandbox runner")
	}
	var command []string
	if instruction.IsJSON {
		command = expandAll(instruction.JSON, build.envOf(state))
	} else {
		command = []string{"sh", "-c", expand(instruction.Args, build.envOf(state))}
	}
	return b.Runner.Run(ctx, RunStep{
		RootfsDir: state.rootfs,
		Command:   command,
		Shell:     !instruction.IsJSON,
		Env:       append([]string{}, state.env...),
		WorkDir:   state.workdir,
		User:      state.user,
		Network:   build.request.Network != "none",
	}, build.progress)
}

func (b *NativeBuilder) applyCopy(ctx context.Context, build *buildContext, state *stageState, instruction dockerfile.Instruction) error {
	flags, sources, dest, err := parseCopy(instruction)
	if err != nil {
		return err
	}
	if len(sources) == 0 || dest == "" {
		return fmt.Errorf("build: %s requires at least one source and a destination", instruction.Name)
	}
	if instruction.Name == "ADD" && isURL(sources[0]) {
		return errors.New("build: ADD from a URL is not supported by the native builder")
	}
	var (
		sourceRoot string
		ignore     *dockerfile.Ignore
	)
	if flags.from != "" {
		root, err := b.stageOrImageRoot(ctx, build, flags.from)
		if err != nil {
			return err
		}
		sourceRoot = root
	} else {
		sourceRoot = build.request.ContextDir
		ignore = build.ignore
	}
	destPath := expand(dest, build.envOf(state))
	if !filepath.IsAbs(destPath) {
		destPath = filepath.Join(state.workdir, destPath)
	}
	target := filepath.Join(state.rootfs, filepath.FromSlash(strings.TrimPrefix(destPath, "/")))
	var owner *fileOwner
	if flags.chown != "" {
		parsed, err := parseOwnership(flags.chown)
		if err != nil {
			return err
		}
		owner = &parsed
	}
	var mode os.FileMode
	if flags.chmod != "" {
		parsed, err := strconv.ParseUint(flags.chmod, 8, 32)
		if err != nil {
			return fmt.Errorf("build: invalid --chmod %q", flags.chmod)
		}
		mode = os.FileMode(parsed)
	}
	record := func(path string, info os.FileInfo) error {
		if owner != nil {
			if rel, err := filepath.Rel(state.rootfs, path); err == nil {
				state.owners[filepath.ToSlash(rel)] = *owner
			}
		}
		if mode != 0 && !info.IsDir() {
			return os.Chmod(path, mode)
		}
		return nil
	}
	multi := len(sources) > 1 || strings.HasSuffix(dest, "/")
	for _, source := range sources {
		matches, err := filepath.Glob(filepath.Join(sourceRoot, filepath.FromSlash(source)))
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			candidate := filepath.Join(sourceRoot, filepath.FromSlash(source))
			if _, statErr := os.Lstat(candidate); statErr != nil {
				return fmt.Errorf("build: %s source %q not found", instruction.Name, source)
			}
			matches = []string{candidate}
		}
		for _, match := range matches {
			info, err := os.Lstat(match)
			if err != nil {
				return err
			}
			dst := target
			if !info.IsDir() && (multi || isDir(target)) {
				dst = filepath.Join(target, filepath.Base(match))
			}
			if err := copyTree(match, dst, copyOptions{base: sourceRoot, ignore: ignore, onEntry: record}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *NativeBuilder) stageOrImageRoot(ctx context.Context, build *buildContext, name string) (string, error) {
	if state, ok := findStage(build, name); ok {
		return state.rootfs, nil
	}
	pulled, err := b.resolveImage(ctx, name, build.request.Pull)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(build.scratch, "from-"+sanitize(name))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := b.Images.Unpack(pulled, dir, oci.UnpackOptions{Context: ctx}); err != nil {
		return "", err
	}
	return dir, nil
}

func (b *NativeBuilder) commitLayer(scratch, name string, base map[string]fileEntry, rootfs string, owners map[string]fileOwner) (*oci.Descriptor, string, error) {
	tmp, err := os.CreateTemp(scratch, name+"-*.tar.gz")
	if err != nil {
		return nil, "", err
	}
	defer os.Remove(tmp.Name())
	gzHash := sha256.New()
	gz, err := gzip.NewWriterLevel(io.MultiWriter(tmp, gzHash), gzip.BestSpeed)
	if err != nil {
		tmp.Close()
		return nil, "", err
	}
	diffHash := sha256.New()
	written, err := writeDiffLayer(base, rootfs, owners, io.MultiWriter(diffHash, gz))
	if err != nil {
		tmp.Close()
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		tmp.Close()
		return nil, "", err
	}
	if written == 0 {
		tmp.Close()
		return nil, "", nil
	}
	info, err := tmp.Stat()
	if err != nil {
		tmp.Close()
		return nil, "", err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return nil, "", err
	}
	blobDigest := "sha256:" + hex.EncodeToString(gzHash.Sum(nil))
	if err := b.CAS.Commit(blobDigest, tmp, info.Size(), b.MaxBlobBytes); err != nil {
		tmp.Close()
		return nil, "", err
	}
	tmp.Close()
	diffID := "sha256:" + hex.EncodeToString(diffHash.Sum(nil))
	return &oci.Descriptor{MediaType: oci.MediaTypeOCILayerGzip, Digest: blobDigest, Size: info.Size()}, diffID, nil
}

func marshalConfig(platform oci.Platform, state *stageState, diffIDs []string, now time.Time) ([]byte, error) {
	config := map[string]any{
		"User":       state.user,
		"Env":        state.env,
		"Entrypoint": state.entrypoint,
		"Cmd":        state.cmd,
		"WorkingDir": state.workdir,
		"Labels":     state.labels,
	}
	if len(state.exposed) > 0 {
		ports := map[string]any{}
		for port := range state.exposed {
			ports[port] = map[string]any{}
		}
		config["ExposedPorts"] = ports
	}
	doc := map[string]any{
		"created":      now.Format(time.RFC3339Nano),
		"architecture": platform.Architecture,
		"os":           platform.OS,
		"config":       config,
		"rootfs":       map[string]any{"type": "layers", "diff_ids": diffIDs},
	}
	if len(state.history) > 0 {
		history := make([]map[string]any, 0, len(state.history))
		for _, entry := range state.history {
			created := entry.Created
			if created == "" {
				created = now.Format(time.RFC3339Nano)
			}
			item := map[string]any{"created": created, "created_by": entry.CreatedBy}
			if entry.EmptyLayer {
				item["empty_layer"] = true
			}
			history = append(history, item)
		}
		doc["history"] = history
	}
	return json.Marshal(doc)
}

func instructionCommand(instruction dockerfile.Instruction) ([]string, error) {
	if instruction.IsJSON {
		return instruction.JSON, nil
	}
	text := strings.TrimSpace(instruction.Args)
	if text == "" {
		return nil, nil
	}
	return []string{"sh", "-c", text}, nil
}

func buildEnvOf(config oci.ImageConfig) []string {
	return append([]string{}, config.Config.Env...)
}

func (build *buildContext) envOf(state *stageState) map[string]string {
	env := map[string]string{}
	for _, entry := range state.env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	for key, value := range build.args {
		if _, ok := env[key]; !ok {
			env[key] = value
		}
	}
	return env
}

func setEnv(env *[]string, key, value string) {
	entry := key + "=" + value
	for i, existing := range *env {
		if strings.HasPrefix(existing, key+"=") {
			(*env)[i] = entry
			return
		}
	}
	*env = append(*env, entry)
}

func findStage(build *buildContext, name string) (*stageState, bool) {
	if state, ok := build.roots[name]; ok {
		return state, true
	}
	if index, err := strconv.Atoi(name); err == nil {
		if state, ok := build.roots[strconv.Itoa(index)]; ok {
			return state, true
		}
	}
	return nil, false
}

func checkSupported(parsed *dockerfile.File) error {
	for _, stage := range parsed.Stages {
		for _, instruction := range stage.Instructions {
			if !dockerfile.Supported(instruction.Name) {
				return fmt.Errorf("build: unsupported Dockerfile instruction %s (line %d)", instruction.Name, instruction.Line)
			}
		}
	}
	return nil
}

func loadIgnore(contextDir string) *dockerfile.Ignore {
	data, err := os.ReadFile(filepath.Join(contextDir, ".dockerignore"))
	if err != nil {
		return dockerfile.ParseIgnore(nil)
	}
	return dockerfile.ParseIgnore(data)
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func expandAll(values []string, env map[string]string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = expand(value, env)
	}
	return out
}

// expand substitutes ${VAR} and $VAR from env, leaving unknown variables intact
// so shell variables in RUN remain meaningful. $$ is an escaped dollar sign.
func expand(text string, env map[string]string) string {
	var builder strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '$' {
			builder.WriteByte(text[i])
			i++
			continue
		}
		if i+1 < len(text) && text[i+1] == '$' {
			builder.WriteByte('$')
			i += 2
			continue
		}
		if i+1 < len(text) && text[i+1] == '{' {
			end := strings.IndexByte(text[i+2:], '}')
			if end < 0 {
				builder.WriteByte(text[i])
				i++
				continue
			}
			name := text[i+2 : i+2+end]
			if value, ok := env[name]; ok {
				builder.WriteString(value)
			} else {
				builder.WriteString(text[i : i+2+end+1])
			}
			i += 2 + end + 1
			continue
		}
		j := i + 1
		for j < len(text) && isNameChar(text[j]) {
			j++
		}
		if j == i+1 {
			builder.WriteByte(text[i])
			i++
			continue
		}
		name := text[i+1 : j]
		if value, ok := env[name]; ok {
			builder.WriteString(value)
		} else {
			builder.WriteString(text[i:j])
		}
		i = j
	}
	return builder.String()
}

func isNameChar(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

type keyValue struct {
	key   string
	value string
}

func parseKeyValues(args string) ([]keyValue, error) {
	fields, err := splitWords(args)
	if err != nil {
		return nil, err
	}
	var pairs []keyValue
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		key, value, found := strings.Cut(field, "=")
		if found {
			pairs = append(pairs, keyValue{key: key, value: value})
			continue
		}
		if i+1 >= len(fields) {
			return nil, fmt.Errorf("build: expected KEY=VALUE, got %q", field)
		}
		pairs = append(pairs, keyValue{key: field, value: fields[i+1]})
		i++
	}
	return pairs, nil
}

// splitWords splits a shell-like argument list honoring single and double quotes.
func splitWords(text string) ([]string, error) {
	var (
		words   []string
		builder strings.Builder
		quote   byte
		started bool
	)
	flush := func() {
		if started {
			words = append(words, builder.String())
			builder.Reset()
			started = false
		}
	}
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
				continue
			}
			if ch == '\\' && quote == '"' && i+1 < len(text) {
				i++
				builder.WriteByte(text[i])
				continue
			}
			builder.WriteByte(ch)
		case ch == '\'' || ch == '"':
			quote = ch
			started = true
		case ch == ' ' || ch == '\t':
			flush()
		case ch == '\\' && i+1 < len(text):
			i++
			builder.WriteByte(text[i])
			started = true
		default:
			builder.WriteByte(ch)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("build: unterminated quote")
	}
	flush()
	return words, nil
}

func parseCopy(instruction dockerfile.Instruction) (copyFlags, []string, string, error) {
	fields := instruction.JSON
	if !instruction.IsJSON {
		var err error
		fields, err = splitWords(instruction.Args)
		if err != nil {
			return copyFlags{}, nil, "", err
		}
	}
	var flags copyFlags
	var rest []string
	for _, field := range fields {
		switch {
		case strings.HasPrefix(field, "--from="):
			flags.from = strings.TrimPrefix(field, "--from=")
		case strings.HasPrefix(field, "--chown="):
			flags.chown = strings.TrimPrefix(field, "--chown=")
		case strings.HasPrefix(field, "--chmod="):
			flags.chmod = strings.TrimPrefix(field, "--chmod=")
		case strings.HasPrefix(field, "--"):
			return copyFlags{}, nil, "", fmt.Errorf("build: unsupported %s flag %q", instruction.Name, field)
		default:
			rest = append(rest, field)
		}
	}
	if len(rest) < 2 {
		return copyFlags{}, nil, "", fmt.Errorf("build: %s requires at least one source and a destination", instruction.Name)
	}
	return flags, rest[:len(rest)-1], rest[len(rest)-1], nil
}

func applyArg(args string, into map[string]string) {
	name, value, found := strings.Cut(strings.TrimSpace(args), "=")
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if _, ok := into[name]; ok {
		return
	}
	if found {
		into[name] = strings.Trim(strings.TrimSpace(value), `"'`)
	} else {
		into[name] = ""
	}
}

func parseOwnership(spec string) (fileOwner, error) {
	owner := fileOwner{}
	uid, gid, found := strings.Cut(spec, ":")
	parsedUID, err := strconv.Atoi(uid)
	if err != nil {
		return owner, fmt.Errorf("build: --chown requires a numeric uid (got %q)", spec)
	}
	owner.uid = parsedUID
	if found {
		parsedGID, err := strconv.Atoi(gid)
		if err != nil {
			return owner, fmt.Errorf("build: --chown requires a numeric gid (got %q)", spec)
		}
		owner.gid = parsedGID
	}
	return owner, nil
}

func isURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		default:
			return '_'
		}
	}, name)
}
