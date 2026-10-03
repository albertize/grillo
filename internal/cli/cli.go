//go:build linux

// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"grillo.local/grillo/internal/api"
	"grillo.local/grillo/internal/model"
	"grillo.local/grillo/internal/observe"
	"grillo.local/grillo/internal/plan"
	"grillo.local/grillo/internal/state"
)

// Client is the subset of the API client the CLI uses.
type Client interface {
	Version(ctx context.Context) (api.VersionInfo, error)
	Health(ctx context.Context) (api.Health, error)
	Apply(ctx context.Context, app model.Application) (string, error)
	Down(ctx context.Context, application string, removeVolumes bool) (string, error)
	Operation(ctx context.Context, id string) (api.Operation, error)
	Status(ctx context.Context, application string) ([]api.ContainerStatus, error)
	Exec(ctx context.Context, application, container string, args []string) (int, string, string, error)
	Events(ctx context.Context, since uint64) (io.ReadCloser, error)
	ListLogs(ctx context.Context, since uint64, resource, container string) ([]observe.LogRecord, error)
	FollowLogs(ctx context.Context, since uint64, resource, container string) (io.ReadCloser, error)
}

// App is the CLI application. Its dependencies are injectable for tests.
type App struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Terminal       Terminal
	SocketPath     string
	DaemonPath     string
	Version        string
	StartupTimeout time.Duration

	ClientFactory func(socketPath string) Client
	Ensure        func(ctx context.Context, socketPath, daemonPath string, timeout time.Duration) error
	Doctor        func() []Check
}

const usage = `usage: grillo <command> [options]

Commands:
  up <manifest>            apply a native manifest (starts the daemon if needed)
  down [--volumes] <app>   stop an application
  plan <manifest>          print the actions for a manifest without applying
  logs <resource> [-f]     show or follow logs
  events [-f]              show or follow the event stream
  exec <pod> [container] -- <command>
  shell <pod> [container]
  doctor                   check host readiness (read-only)
  version
`

func (a *App) withDefaults() {
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Terminal == nil {
		a.Terminal = NewTerminal(int(os.Stdin.Fd()))
	}
	if a.SocketPath == "" {
		a.SocketPath = DefaultSocketPath()
	}
	if a.StartupTimeout == 0 {
		a.StartupTimeout = 15 * time.Second
	}
	if a.ClientFactory == nil {
		a.ClientFactory = func(socketPath string) Client { return api.NewClient(socketPath) }
	}
	if a.Ensure == nil {
		a.Ensure = func(ctx context.Context, socketPath, daemonPath string, timeout time.Duration) error {
			return api.EnsureDaemon(ctx, socketPath, daemonPath, timeout)
		}
	}
	if a.Doctor == nil {
		a.Doctor = DoctorChecks
	}
}

// Run dispatches a command and returns an exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	a.withDefaults()
	if len(args) == 0 {
		fmt.Fprint(a.Stdout, usage)
		return 0
	}
	switch args[0] {
	case "version", "--version":
		version := a.Version
		if version == "" {
			version = "dev"
		}
		fmt.Fprintln(a.Stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(a.Stdout, usage)
		return 0
	case "doctor":
		return PrintDoctor(a.Stdout, a.Doctor())
	case "plan":
		return a.cmdPlan(ctx, args[1:])
	case "up":
		return a.cmdUp(ctx, args[1:])
	case "down":
		return a.cmdDown(ctx, args[1:])
	case "status":
		return a.cmdStatus(ctx, args[1:])
	case "inspect":
		return a.cmdInspect(ctx, args[1:])
	case "logs":
		return a.cmdLogs(ctx, args[1:])
	case "events":
		return a.cmdEvents(ctx, args[1:])
	case "exec":
		return a.cmdExec(ctx, args[1:], false)
	case "shell":
		return a.cmdExec(ctx, args[1:], true)
	default:
		fmt.Fprintf(a.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func (a *App) cmdPlan(_ context.Context, args []string) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var files stringSlice
	fs.Var(&files, "f", "manifest file (repeatable)")
	output := fs.String("output", "text", "text or json")
	flags, positionals, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "plan", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	app, code := a.loadManifest(files, positionals)
	if code != 0 {
		return code
	}
	result, err := plan.Build(app, plan.Observed{Sandboxes: map[string]plan.ObservedSandbox{}, Volumes: map[string]bool{}})
	if err != nil {
		return fail(a.Stderr, err)
	}
	if *output == "json" {
		encoder := json.NewEncoder(a.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return fail(a.Stderr, err)
		}
		return 0
	}
	fmt.Fprintf(a.Stdout, "Plan for application %q:\n", app.Identity.Name)
	if result.Empty() {
		fmt.Fprintln(a.Stdout, "  (no actions)")
		return 0
	}
	for _, action := range result.Actions {
		fmt.Fprintf(a.Stdout, "  %-16s %-28s %s\n", action.Kind, action.Resource, action.Reason)
	}
	return 0
}

func (a *App) cmdUp(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	var files stringSlice
	fs.Var(&files, "f", "manifest file (repeatable)")
	flags, positionals, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "up", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	app, code := a.loadManifest(files, positionals)
	if code != 0 {
		return code
	}
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	client := a.ClientFactory(a.SocketPath)
	id, err := client.Apply(ctx, app)
	if err != nil {
		return fail(a.Stderr, err)
	}
	return a.waitOperation(ctx, client, id)
}

func (a *App) cmdStatus(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	output := fs.String("output", "text", "text or json")
	flags, positionals, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "status", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positionals) != 1 {
		fmt.Fprintln(a.Stderr, "usage: grillo status <application>")
		return 2
	}
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	containers, err := a.ClientFactory(a.SocketPath).Status(ctx, positionals[0])
	if err != nil {
		return fail(a.Stderr, err)
	}
	if *output == "json" {
		encoder := json.NewEncoder(a.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]any{"application": positionals[0], "containers": containers}); err != nil {
			return fail(a.Stderr, err)
		}
		return 0
	}
	if len(containers) == 0 {
		fmt.Fprintf(a.Stdout, "%s: no containers\n", positionals[0])
		return 0
	}
	for _, container := range containers {
		fmt.Fprintf(a.Stdout, "%s/%s\t%s\n", positionals[0], container.Container, container.State)
	}
	return 0
}

func (a *App) cmdInspect(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	flags, positionals, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "inspect", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positionals) != 1 {
		fmt.Fprintln(a.Stderr, "usage: grillo inspect <application>")
		return 2
	}
	application := strings.TrimPrefix(positionals[0], "application/")
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	containers, err := a.ClientFactory(a.SocketPath).Status(ctx, application)
	if err != nil {
		return fail(a.Stderr, err)
	}
	encoder := json.NewEncoder(a.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"application": application, "containers": containers}); err != nil {
		return fail(a.Stderr, err)
	}
	return 0
}

func (a *App) cmdDown(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	removeVolumes := fs.Bool("volumes", false, "delete owned managed volumes")
	flags, positionals, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "down", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positionals) != 1 {
		fmt.Fprintf(a.Stderr, "usage: grillo down [--volumes] <application>\n")
		return 2
	}
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	client := a.ClientFactory(a.SocketPath)
	id, err := client.Down(ctx, positionals[0], *removeVolumes)
	if err != nil {
		return fail(a.Stderr, err)
	}
	return a.waitOperation(ctx, client, id)
}

func (a *App) cmdLogs(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	follow := fs.Bool("follow", false, "follow log output")
	fs.BoolVar(follow, "f", false, "follow log output")
	container := fs.String("container", "", "container name")
	flags, positionals, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "logs", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positionals) != 1 {
		fmt.Fprintf(a.Stderr, "usage: grillo logs <resource> [-f]\n")
		return 2
	}
	resource := positionals[0]
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	client := a.ClientFactory(a.SocketPath)
	if *follow {
		stream, err := client.FollowLogs(ctx, 0, resource, *container)
		if err != nil {
			return fail(a.Stderr, err)
		}
		defer stream.Close()
		return a.printSSE(stream, func(data []byte) {
			var record observe.LogRecord
			if json.Unmarshal(data, &record) == nil {
				fmt.Fprintf(a.Stdout, "%s %s\n", record.Container, strings.TrimRight(record.Line, "\n"))
			}
		})
	}
	records, err := client.ListLogs(ctx, 0, resource, *container)
	if err != nil {
		return fail(a.Stderr, err)
	}
	for _, record := range records {
		fmt.Fprintf(a.Stdout, "%s %s\n", record.Container, strings.TrimRight(record.Line, "\n"))
	}
	return 0
}

func (a *App) cmdEvents(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	follow := fs.Bool("follow", false, "follow events")
	fs.BoolVar(follow, "f", false, "follow events")
	flags, _, _, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, "events", err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	client := a.ClientFactory(a.SocketPath)
	stream, err := client.Events(ctx, 0)
	if err != nil {
		return fail(a.Stderr, err)
	}
	defer stream.Close()
	return a.printSSE(stream, func(data []byte) {
		var event observe.Event
		if json.Unmarshal(data, &event) == nil {
			fmt.Fprintf(a.Stdout, "%d %s %s\n", event.Sequence, event.Kind, event.Message)
		}
	})
}

func (a *App) cmdExec(ctx context.Context, args []string, shell bool) int {
	name := "exec"
	if shell {
		name = "shell"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	flags, positionals, afterDD, err := SplitForFlagSet(args, fs)
	if err != nil {
		return usageError(a.Stderr, name, err)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positionals) == 0 {
		fmt.Fprintf(a.Stderr, "usage: grillo %s <pod> [container]%s\n", name, map[bool]string{true: "", false: " -- <command>"}[shell])
		return 2
	}
	if !shell && len(afterDD) == 0 {
		fmt.Fprintf(a.Stderr, "usage: grillo exec <pod> [container] -- <command>\n")
		return 2
	}
	if shell {
		fmt.Fprintln(a.Stderr, "grillo: interactive shell sessions are not supported yet; use exec -- <command>")
		return 1
	}
	application := strings.TrimPrefix(positionals[0], "pod/")
	container := ""
	if len(positionals) > 1 {
		container = positionals[1]
	}
	if container == "" {
		fmt.Fprintln(a.Stderr, "grillo: a container name is required")
		return 2
	}
	if code := a.ensureDaemon(ctx); code != 0 {
		return code
	}
	// Put the local terminal in raw mode and always restore it, even if the
	// session fails, so an interrupted CLI never leaves a broken terminal.
	if a.Terminal.IsTerminal() {
		restore, err := a.Terminal.MakeRaw()
		if err != nil {
			return fail(a.Stderr, err)
		}
		defer restore()
	}
	exitCode, stdout, stderr, err := a.ClientFactory(a.SocketPath).Exec(ctx, application, container, afterDD)
	if err != nil {
		return fail(a.Stderr, err)
	}
	fmt.Fprint(a.Stdout, stdout)
	fmt.Fprint(a.Stderr, stderr)
	return exitCode
}

func (a *App) ensureDaemon(ctx context.Context) int {
	if err := a.Ensure(ctx, a.SocketPath, a.DaemonPath, a.StartupTimeout); err != nil {
		return fail(a.Stderr, err)
	}
	return 0
}

func (a *App) waitOperation(ctx context.Context, client Client, id string) int {
	interval := 100 * time.Millisecond
	for {
		operation, err := client.Operation(ctx, id)
		if err != nil {
			return fail(a.Stderr, err)
		}
		switch operation.State {
		case "succeeded":
			fmt.Fprintf(a.Stdout, "%s: %s\n", operation.Kind, operation.State)
			return 0
		case "failed":
			message := "operation failed"
			if operation.Error != nil {
				message = operation.Error.Error()
			}
			return fail(a.Stderr, fmt.Errorf("%s: %s", operation.Kind, message))
		case "canceled":
			return fail(a.Stderr, fmt.Errorf("%s: canceled", operation.Kind))
		}
		select {
		case <-ctx.Done():
			return fail(a.Stderr, ctx.Err())
		case <-time.After(interval):
		}
	}
}

func (a *App) printSSE(stream io.Reader, handle func([]byte)) int {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			handle([]byte(data))
		}
	}
	// A stream ending (including a canceled context) is a normal termination.
	return 0
}

// DefaultSocketPath returns the daemon socket path under the runtime directory.
func DefaultSocketPath() string {
	layout, err := state.NewLayout(state.DefaultConfig())
	if err != nil {
		return ""
	}
	return filepath.Join(layout.Runtime, "grillod.sock")
}

func (a *App) loadManifest(files stringSlice, positionals []string) (model.Application, int) {
	sources := append([]string(nil), positionals...)
	if len(files) > 0 {
		sources = append(sources, files...)
	}
	if len(sources) == 0 {
		fmt.Fprintln(a.Stderr, "grillo: no manifest given")
		return model.Application{}, 2
	}
	if len(sources) > 1 {
		fmt.Fprintln(a.Stderr, "grillo: multiple manifest sources are not supported yet")
		return model.Application{}, 2
	}
	var data []byte
	var err error
	if sources[0] == "-" {
		data, err = io.ReadAll(io.LimitReader(a.Stdin, 8<<20))
	} else {
		data, err = os.ReadFile(sources[0])
	}
	if err != nil {
		return model.Application{}, fail(a.Stderr, err)
	}
	app, diagnostics, err := model.LoadManifest(data)
	if err != nil {
		return model.Application{}, fail(a.Stderr, err)
	}
	diagnostics.Sort()
	validation := model.Validate(app, model.Capabilities{})
	validation.Sort()
	all := append(diagnostics, validation...)
	if len(all) > 0 {
		for _, diagnostic := range all {
			fmt.Fprintln(a.Stderr, diagnostic.Error())
		}
	}
	if all.HasErrors() {
		return model.Application{}, 1
	}
	return app, 0
}

func usageError(w io.Writer, command string, err error) int {
	fmt.Fprintf(w, "grillo %s: %v\n", command, err)
	return 2
}

func fail(w io.Writer, err error) int {
	fmt.Fprintf(w, "grillo: %v\n", err)
	return 1
}
