// SPDX-License-Identifier: Apache-2.0

// Package dockerfile parses a supported Dockerfile subset into an ordered
// instruction list. It performs no I/O and no instruction execution, so parsing
// is pure and testable. Unknown or unsupported instructions are preserved and
// reported by the builder as structured diagnostics rather than silently
// ignored.
package dockerfile

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Instruction is one Dockerfile instruction with continuations joined.
type Instruction struct {
	Name   string   // upper-cased, e.g. "RUN"
	Args   string   // raw argument text after the name
	JSON   []string // parsed JSON-array arguments, when the form is JSON
	IsJSON bool
	Line   int
}

// Stage is a build stage introduced by FROM.
type Stage struct {
	Index        int
	Name         string
	Base         string // image reference or "scratch"
	Platform     string
	Instructions []Instruction
}

// File is a parsed Dockerfile.
type File struct {
	Stages     []Stage
	GlobalArgs map[string]string
	Escape     byte
}

// Supported reports whether an instruction name is handled by the native
// builder. It is intentionally separate from parsing so the builder can emit a
// diagnostic instead of failing to parse.
func Supported(name string) bool {
	switch strings.ToUpper(name) {
	case "FROM", "ARG", "ENV", "WORKDIR", "USER", "COPY", "ADD", "RUN",
		"CMD", "ENTRYPOINT", "EXPOSE", "LABEL":
		return true
	default:
		return false
	}
}

// Parse parses Dockerfile bytes.
func Parse(data []byte) (*File, error) {
	file := &File{GlobalArgs: map[string]string{}, Escape: '\\'}
	lines, err := logicalLines(string(data), &file.Escape)
	if err != nil {
		return nil, err
	}
	var (
		stages  []Stage
		current *Stage
	)
	for _, line := range lines {
		text := strings.TrimSpace(line.text)
		if text == "" {
			continue
		}
		name, args := splitInstruction(text)
		upper := strings.ToUpper(name)
		if upper == "FROM" {
			stage, err := parseFrom(args, len(stages), line.number)
			if err != nil {
				return nil, err
			}
			stages = append(stages, stage)
			current = &stages[len(stages)-1]
			continue
		}
		instruction := Instruction{Name: upper, Args: args, Line: line.number}
		if values, ok := parseJSONArray(args); ok {
			instruction.JSON = values
			instruction.IsJSON = true
		}
		if current == nil {
			// Only a global ARG is valid before the first FROM.
			if upper != "ARG" {
				return nil, fmt.Errorf("dockerfile: line %d: %s before FROM", line.number, upper)
			}
			parseArg(args, file.GlobalArgs)
			continue
		}
		current.Instructions = append(current.Instructions, instruction)
	}
	file.Stages = stages
	return file, nil
}

type logicalLine struct {
	text   string
	number int
}

// logicalLines strips comments, then joins escaped continuations.
func logicalLines(data string, escape *byte) ([]logicalLine, error) {
	raw := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	// Directives may set the escape character before any instruction.
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		if name, value, ok := strings.Cut(strings.TrimPrefix(trimmed, "#"), "="); ok {
			if strings.TrimSpace(name) == "escape" {
				value = strings.TrimSpace(value)
				if len(value) != 1 || (value[0] != '\\' && value[0] != '`') {
					return nil, fmt.Errorf("dockerfile: invalid escape directive %q", value)
				}
				*escape = value[0]
			}
		}
	}
	var lines []logicalLine
	var builder strings.Builder
	startLine := 0
	pending := false
	for i, line := range raw {
		trimmed := strings.TrimSpace(line)
		if !pending && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
			continue
		}
		if !pending {
			startLine = i + 1
		}
		content := line
		if endsWithContinuation(content, *escape) {
			content = stripContinuation(content, *escape)
			builder.WriteString(content)
			pending = true
			continue
		}
		builder.WriteString(content)
		lines = append(lines, logicalLine{text: builder.String(), number: startLine})
		builder.Reset()
		pending = false
	}
	if pending {
		return nil, fmt.Errorf("dockerfile: line %d: unterminated continuation", startLine)
	}
	return lines, nil
}

func endsWithContinuation(line string, escape byte) bool {
	trimmed := strings.TrimRight(line, " \t")
	if len(trimmed) == 0 {
		return false
	}
	// An escaped escape is not a continuation.
	if trimmed[len(trimmed)-1] != escape {
		return false
	}
	count := 0
	for i := len(trimmed) - 1; i >= 0 && trimmed[i] == escape; i-- {
		count++
	}
	return count%2 == 1
}

func stripContinuation(line string, escape byte) string {
	trimmed := strings.TrimRight(line, " \t")
	return trimmed[:len(trimmed)-1]
}

func splitInstruction(text string) (string, string) {
	index := strings.IndexAny(text, " \t")
	if index < 0 {
		return text, ""
	}
	return text[:index], strings.TrimSpace(text[index+1:])
}

func parseFrom(args string, index, line int) (Stage, error) {
	fields := strings.Fields(args)
	stage := Stage{Index: index}
	var base string
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		switch {
		case strings.HasPrefix(field, "--platform="):
			stage.Platform = strings.TrimPrefix(field, "--platform=")
		case strings.HasPrefix(field, "--"):
			return Stage{}, fmt.Errorf("dockerfile: line %d: unsupported FROM flag %q", line, field)
		case strings.EqualFold(field, "AS"):
			if i+1 >= len(fields) {
				return Stage{}, fmt.Errorf("dockerfile: line %d: FROM AS without a name", line)
			}
			stage.Name = fields[i+1]
			i++
		default:
			if base != "" {
				return Stage{}, fmt.Errorf("dockerfile: line %d: unexpected token %q in FROM", line, field)
			}
			base = field
		}
	}
	if base == "" {
		return Stage{}, fmt.Errorf("dockerfile: line %d: FROM requires a base image", line)
	}
	stage.Base = base
	return stage, nil
}

func parseJSONArray(args string) ([]string, bool) {
	trimmed := strings.TrimSpace(args)
	if !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var values []string
	if err := json.Unmarshal([]byte(trimmed), &values); err != nil {
		return nil, false
	}
	return values, true
}

func parseArg(args string, into map[string]string) {
	text := strings.TrimSpace(args)
	if text == "" {
		return
	}
	name, value, found := strings.Cut(text, "=")
	name = strings.TrimSpace(name)
	if !found {
		if _, ok := into[name]; !ok {
			into[name] = ""
		}
		return
	}
	into[name] = strings.Trim(value, `"'`)
}
