// SPDX-License-Identifier: Apache-2.0

package guestproto

import (
	"encoding/json"
	"fmt"
	"time"
)

// MessageType names a control message. Requests use the present tense
// (status, probe, exec) and replies use a _result suffix (status_result,
// probe_result, exec_result).
type MessageType string

const (
	TypeHello        MessageType = "hello"
	TypeHelloAck     MessageType = "hello_ack"
	TypeAuth         MessageType = "auth"
	TypeAuthOK       MessageType = "auth_ok"
	TypeError        MessageType = "error"
	TypePing         MessageType = "ping"
	TypePong         MessageType = "pong"
	TypeCancel       MessageType = "cancel"
	TypeStart        MessageType = "start"
	TypeStarted      MessageType = "started"
	TypeStop         MessageType = "stop"
	TypeStopped      MessageType = "stopped"
	TypeStatus       MessageType = "status"
	TypeStatusResult MessageType = "status_result"
	TypeProbe        MessageType = "probe"
	TypeProbeResult  MessageType = "probe_result"
	TypeExec         MessageType = "exec"
	TypeExecResult   MessageType = "exec_result"
	TypeRestart      MessageType = "restart"
	TypeRestarted    MessageType = "restarted"
	TypeRun          MessageType = "run"
	TypeRunResult    MessageType = "run_result"
)

func (t MessageType) isResponse() bool {
	switch t {
	case TypeHelloAck, TypeAuth, TypeAuthOK, TypeError, TypePong,
		TypeStarted, TypeStopped, TypeStatusResult, TypeProbeResult, TypeExecResult, TypeRestarted, TypeRunResult:
		return true
	default:
		return false
	}
}

// ErrorCode is a stable, machine-readable error identifier.
type ErrorCode string

const (
	CodeUnauthorized     ErrorCode = "unauthorized"
	CodeVersionMismatch  ErrorCode = "version_mismatch"
	CodeBadRequest       ErrorCode = "bad_request"
	CodeUnknownType      ErrorCode = "unknown_type"
	CodeNotFound         ErrorCode = "not_found"
	CodeCanceled         ErrorCode = "canceled"
	CodeDeadlineExceeded ErrorCode = "deadline_exceeded"
	CodeBusy             ErrorCode = "busy"
	CodeUnsupported      ErrorCode = "unsupported"
	CodeInternal         ErrorCode = "internal"
)

// Error is a typed protocol error carried by an error message or a reply.
type Error struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message,omitempty"`
	Retryable bool      `json:"retryable,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// AsError extracts a typed protocol error from err, if present.
func AsError(err error) (*Error, bool) {
	pe, ok := err.(*Error)
	return pe, ok
}

// Message is the control-message envelope. Requests and responses correlate by
// ID; ReplyTo references the request being answered or canceled.
type Message struct {
	ID       string          `json:"id,omitempty"`
	Type     MessageType     `json:"type"`
	ReplyTo  string          `json:"replyTo,omitempty"`
	Deadline *time.Time      `json:"deadline,omitempty"`
	Error    *Error          `json:"error,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// MarshalPayload encodes v into a message payload.
func MarshalPayload(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("guestproto: encode payload: %w", err)
	}
	return data, nil
}

// UnmarshalPayload decodes a message payload into v. A nil payload is a no-op.
func UnmarshalPayload(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrCorrupt, err)
	}
	return nil
}

// ProbeKind selects a probe implementation.
type ProbeKind string

const (
	ProbeExec ProbeKind = "exec"
	ProbeHTTP ProbeKind = "http"
	ProbeTCP  ProbeKind = "tcp"
)

// ProbeRequest describes a health probe evaluated inside the guest.
type ProbeRequest struct {
	Kind      ProbeKind `json:"kind"`
	Container string    `json:"container,omitempty"`
	Command   []string  `json:"command,omitempty"`
	URL       string    `json:"url,omitempty"`
	Host      string    `json:"host,omitempty"`
	Port      int       `json:"port,omitempty"`
	TimeoutMS int64     `json:"timeoutMs,omitempty"`
}

// ProbeResult reports a probe outcome.
type ProbeResult struct {
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail,omitempty"`
}

// ContainerStatus is one container's observed state.
type ContainerStatus struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	ExitCode int    `json:"exitCode,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

// StatusResult reports guest/sandbox state.
type StatusResult struct {
	State      string            `json:"state"`
	UptimeMS   int64             `json:"uptimeMs,omitempty"`
	Zombies    int               `json:"zombies"`
	Containers []ContainerStatus `json:"containers,omitempty"`
}

// StartRequest asks the guest to start its workload. Sandbox carries the
// resolved container specs; a nil sandbox asks the guest to start the workload
// it was booted with.
type StartRequest struct {
	Sandbox   *SandboxSpec `json:"sandbox,omitempty"`
	TimeoutMS int64        `json:"timeoutMs,omitempty"`
}

// StartResult acknowledges a start.
type StartResult struct {
	State      string            `json:"state"`
	Containers []ContainerStatus `json:"containers,omitempty"`
}

// StopRequest asks the guest to stop its workload.
type StopRequest struct {
	TimeoutMS int64 `json:"timeoutMs,omitempty"`
}

// StopResult acknowledges a stop.
type StopResult struct {
	Stopped bool `json:"stopped"`
}

// ExecRequest runs a command in the guest. Session identifies the stream group;
// the client assigns one when zero.
type ExecRequest struct {
	Session   uint32   `json:"session,omitempty"`
	Container string   `json:"container,omitempty"`
	Args      []string `json:"args"`
	Env       []string `json:"env,omitempty"`
	Dir       string   `json:"dir,omitempty"`
	TTY       bool     `json:"tty,omitempty"`
	Stdin     bool     `json:"stdin,omitempty"`
	TimeoutMS int64    `json:"timeoutMs,omitempty"`
}

// ExecResult is the terminal result of an exec.
type ExecResult struct {
	ExitCode int    `json:"exitCode"`
	Detail   string `json:"detail,omitempty"`
}

// RunRequest runs one build command against a mounted build share.
type RunRequest struct {
	Session   uint32   `json:"session,omitempty"`
	Share     string   `json:"share"`
	Args      []string `json:"args"`
	Env       []string `json:"env,omitempty"`
	WorkDir   string   `json:"workDir,omitempty"`
	User      UserSpec `json:"user,omitempty"`
	TimeoutMS int64    `json:"timeoutMs,omitempty"`
}

// RunResult is the terminal result of a build run.
type RunResult struct {
	ExitCode int `json:"exitCode"`
}

// RestartRequest restarts one container in place.
type RestartRequest struct {
	Container string `json:"container"`
}

// RestartResult acknowledges a restart.
type RestartResult struct {
	State string `json:"state"`
}

// CancelRequest cancels a pending request by ID.
type CancelRequest struct {
	Target string `json:"target"`
}
