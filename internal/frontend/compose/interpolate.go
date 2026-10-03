// SPDX-License-Identifier: Apache-2.0

package compose

import (
	"fmt"
	"strings"
)

// Interpolate expands Compose variable references in a string. Supported forms:
//
//	$VAR ${VAR} ${VAR:-default} ${VAR-default} ${VAR:?err} ${VAR?err}
//	${VAR:+alt} ${VAR+alt} and the $$ escape.
//
// It returns warnings for variables left unset without a default (Compose
// substitutes an empty string and warns) and an error for the required form.
func Interpolate(input string, env map[string]string) (string, []string, error) {
	var out strings.Builder
	var warnings []string
	for i := 0; i < len(input); {
		c := input[i]
		if c != '$' {
			out.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(input) {
			out.WriteByte('$')
			break
		}
		next := input[i+1]
		if next == '$' {
			out.WriteByte('$')
			i += 2
			continue
		}
		if next == '{' {
			end := strings.IndexByte(input[i+2:], '}')
			if end < 0 {
				return "", warnings, fmt.Errorf("unterminated variable expression at position %d", i)
			}
			expression := input[i+2 : i+2+end]
			value, warning, err := expandBraced(expression, env)
			if err != nil {
				return "", warnings, err
			}
			if warning != "" {
				warnings = append(warnings, warning)
			}
			out.WriteString(value)
			i += 2 + end + 1
			continue
		}
		if isVarStart(next) {
			j := i + 1
			for j < len(input) && isVarChar(input[j]) {
				j++
			}
			name := input[i+1 : j]
			if value, ok := env[name]; ok {
				out.WriteString(value)
			} else {
				warnings = append(warnings, "variable "+name+" is not set; using an empty string")
			}
			i = j
			continue
		}
		out.WriteByte('$')
		i++
	}
	return out.String(), warnings, nil
}

func expandBraced(expression string, env map[string]string) (string, string, error) {
	name := expression
	operator := ""
	argument := ""
	for _, op := range []string{":-", ":?", ":+", "-", "?", "+"} {
		if index := strings.Index(expression, op); index >= 0 {
			name = expression[:index]
			operator = op
			argument = expression[index+len(op):]
			break
		}
	}
	value, set := env[name]
	nonEmpty := set && value != ""
	switch operator {
	case ":-":
		if nonEmpty {
			return value, "", nil
		}
		return argument, "", nil
	case "-":
		if set {
			return value, "", nil
		}
		return argument, "", nil
	case ":+":
		if nonEmpty {
			return argument, "", nil
		}
		return "", "", nil
	case "+":
		if set {
			return argument, "", nil
		}
		return "", "", nil
	case ":?":
		if nonEmpty {
			return value, "", nil
		}
		return "", "", fmt.Errorf("required variable %s is missing or empty: %s", name, argument)
	case "?":
		if set {
			return value, "", nil
		}
		return "", "", fmt.Errorf("required variable %s is missing: %s", name, argument)
	default:
		if set {
			return value, "", nil
		}
		return "", "variable " + name + " is not set; using an empty string", nil
	}
}

func isVarStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isVarChar(c byte) bool {
	return isVarStart(c) || (c >= '0' && c <= '9')
}
