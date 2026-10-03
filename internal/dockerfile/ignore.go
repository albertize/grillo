// SPDX-License-Identifier: Apache-2.0

package dockerfile

import (
	"regexp"
	"strings"
)

// Ignore is a compiled .dockerignore pattern list. It matches Docker's
// semantics closely: patterns are relative to the build context root, `*` does
// not cross a path separator, `**` matches across separators, and `!` re-includes.
// A pattern also matches when it matches a parent directory of the path.
type Ignore struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	re     *regexp.Regexp
	negate bool
}

// ParseIgnore compiles .dockerignore content. Invalid patterns are skipped
// rather than failing the build, matching Docker's tolerance.
func ParseIgnore(data []byte) *Ignore {
	ignore := &Ignore{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		negate := false
		if strings.HasPrefix(pattern, "!") {
			negate = true
			pattern = strings.TrimSpace(pattern[1:])
		}
		if pattern == "" {
			continue
		}
		// A leading slash anchors to the context root, which is already the case.
		pattern = strings.TrimPrefix(pattern, "/")
		// A trailing slash marks a directory; the regex still matches the name.
		pattern = strings.TrimSuffix(pattern, "/")
		pattern = strings.TrimPrefix(pattern, "./")
		if pattern == "" {
			continue
		}
		re, err := compilePattern(pattern)
		if err != nil {
			continue
		}
		ignore.patterns = append(ignore.patterns, ignorePattern{re: re, negate: negate})
	}
	return ignore
}

// Empty reports whether the ignore list has no patterns.
func (i *Ignore) Empty() bool { return i == nil || len(i.patterns) == 0 }

// Excluded reports whether a context-relative path is excluded.
func (i *Ignore) Excluded(relPath string) bool {
	if i == nil {
		return false
	}
	relPath = strings.TrimPrefix(cleanRelative(relPath), "./")
	ignored := false
	for _, pattern := range i.patterns {
		if pattern.matchesOrParent(relPath) {
			ignored = !pattern.negate
		}
	}
	return ignored
}

func (p ignorePattern) matchesOrParent(path string) bool {
	if p.re.MatchString(path) {
		return true
	}
	for parent := parentPath(path); parent != ""; parent = parentPath(parent) {
		if p.re.MatchString(parent) {
			return true
		}
	}
	return false
}

func parentPath(path string) string {
	index := strings.LastIndex(path, "/")
	if index < 0 {
		return ""
	}
	return path[:index]
}

func cleanRelative(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

// compilePattern translates a Docker ignore pattern into an anchored regexp.
func compilePattern(pattern string) (*regexp.Regexp, error) {
	var builder strings.Builder
	builder.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				// Consume an optional following slash as part of the wildcard.
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					builder.WriteString("(.*/)?")
				} else {
					builder.WriteString(".*")
				}
			} else {
				builder.WriteString("[^/]*")
			}
		case '?':
			builder.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '\\':
			builder.WriteByte('\\')
			builder.WriteByte(ch)
		case '[':
			// Copy a character class verbatim up to the closing bracket.
			end := strings.IndexByte(pattern[i:], ']')
			if end < 0 {
				builder.WriteString("\\[")
				continue
			}
			builder.WriteString(pattern[i : i+end+1])
			i += end
		default:
			builder.WriteByte(ch)
		}
	}
	builder.WriteString("$")
	return regexp.Compile(builder.String())
}
