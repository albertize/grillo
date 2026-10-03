// SPDX-License-Identifier: Apache-2.0

package dockerfile

import (
	"strings"
	"testing"
)

func TestParseBasic(t *testing.T) {
	file, err := Parse([]byte(`
# comment
FROM alpine:3.20 AS build
ARG VERSION=1.2
RUN echo hello && \
    echo world
COPY --from=build /src /dst
ENV A=1 B="two words"
WORKDIR /app
USER 1000:1000
EXPOSE 8080
CMD ["sh", "-c", "echo done"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Stages) != 1 {
		t.Fatalf("stages = %d", len(file.Stages))
	}
	stage := file.Stages[0]
	if stage.Base != "alpine:3.20" || stage.Name != "build" {
		t.Fatalf("stage = %+v", stage)
	}
	if len(stage.Instructions) != 8 {
		t.Fatalf("instructions = %d: %+v", len(stage.Instructions), stage.Instructions)
	}
	if stage.Instructions[1].Args != "echo hello &&     echo world" && stage.Instructions[1].Args != "echo hello && echo world" {
		t.Fatalf("run args = %q", stage.Instructions[1].Args)
	}
	last := stage.Instructions[len(stage.Instructions)-1]
	if !last.IsJSON || last.Name != "CMD" || len(last.JSON) != 3 {
		t.Fatalf("cmd = %+v", last)
	}
}

func TestParseMultiStageAndGlobalArg(t *testing.T) {
	file, err := Parse([]byte("ARG BASE=alpine\nFROM ${BASE} AS first\nFROM first AS second\nCOPY --from=first /a /b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if file.GlobalArgs["BASE"] != "alpine" {
		t.Fatalf("global args = %+v", file.GlobalArgs)
	}
	if len(file.Stages) != 2 || file.Stages[1].Base != "first" {
		t.Fatalf("stages = %+v", file.Stages)
	}
	if file.Stages[1].Instructions[0].Args != "--from=first /a /b" {
		t.Fatalf("copy = %+v", file.Stages[1].Instructions[0])
	}
}

func TestParseFromPlatform(t *testing.T) {
	file, err := Parse([]byte("FROM --platform=linux/arm64 alpine AS base\n"))
	if err != nil {
		t.Fatal(err)
	}
	if file.Stages[0].Platform != "linux/arm64" {
		t.Fatalf("platform = %q", file.Stages[0].Platform)
	}
}

func TestParseEscapeDirective(t *testing.T) {
	file, err := Parse([]byte("# escape=`\nFROM scratch\nCOPY a`\nb /dst\n"))
	if err != nil {
		t.Fatal(err)
	}
	if file.Escape != '`' {
		t.Fatalf("escape = %q", file.Escape)
	}
	if len(file.Stages[0].Instructions) != 1 {
		t.Fatalf("instructions = %+v", file.Stages[0].Instructions)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("RUN echo before from\n")); err == nil {
		t.Fatal("instruction before FROM was accepted")
	}
	if _, err := Parse([]byte("FROM\n")); err == nil {
		t.Fatal("FROM without a base was accepted")
	}
	if _, err := Parse([]byte("FROM scratch\nRUN echo \\\n")); err == nil {
		t.Fatal("unterminated continuation was accepted")
	}
	if _, err := Parse([]byte("FROM --bogus=x alpine\n")); err == nil {
		t.Fatal("unsupported FROM flag was accepted")
	}
}

func TestSupportedInstructions(t *testing.T) {
	for _, name := range []string{"FROM", "RUN", "COPY", "ADD", "ENV", "ARG", "WORKDIR", "USER", "CMD", "ENTRYPOINT", "EXPOSE", "LABEL"} {
		if !Supported(name) {
			t.Errorf("%s should be supported", name)
		}
	}
	for _, name := range []string{"HEALTHCHECK", "SHELL", "ONBUILD", "VOLUME", "STOPSIGNAL", "MAINTAINER"} {
		if Supported(name) {
			t.Errorf("%s should not be supported", name)
		}
	}
}

func TestLogicalLinesJoin(t *testing.T) {
	var escape byte = '\\'
	lines, _ := logicalLines("RUN a \\\n b\n", &escape)
	if len(lines) != 1 || !strings.Contains(lines[0].text, "a") || !strings.Contains(lines[0].text, "b") {
		t.Fatalf("lines = %+v", lines)
	}
}
