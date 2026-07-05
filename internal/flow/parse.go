package flow

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

//go:embed prelude.js
var preludeJS string

var (
	exportDefaultRe = regexp.MustCompile(`(?m)^(\s*)export\s+default\s+`)
	// Only strip `export` when it precedes a declaration keyword at the start of a
	// line. Requiring the keyword avoids mangling a line inside a multiline string
	// that happens to start with the word "export" (e.g. `export pending`).
	exportRe     = regexp.MustCompile(`(?m)^(\s*)export\s+(const|let|var|function|async\s+function|class)\b`)
	metaAssignRe = regexp.MustCompile(`(?m)\bmeta\s*=\s*\{`)
)

// Validate pre-flights a workflow source WITHOUT running it: it checks the `meta`
// block parses and that the full script compiles as JavaScript. Use it to reject an
// agent-generated or user-supplied script with a clear error before spawning any
// agents. (Engine.Run compiles too, so a bad script never runs; Validate just moves
// that check up-front with a friendlier message.)
func Validate(source string) error {
	if _, err := ParseMeta(source); err != nil {
		return err
	}
	if _, err := goja.Compile("workflow.js", wrapSource(source), false); err != nil {
		return fmt.Errorf("workflow script does not compile: %w", err)
	}
	return nil
}

// ParseMeta extracts the `meta` object literal from a workflow's source and
// evaluates it in a throwaway VM (without running the body). The meta block must
// be a pure literal (no computed values, no host calls) — this is enforced by
// running it in a bare runtime with no globals injected.
func ParseMeta(src string) (Meta, error) {
	lit, err := extractMetaLiteral(src)
	if err != nil {
		return Meta{}, err
	}
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	v, err := vm.RunString("(" + lit + ")")
	if err != nil {
		return Meta{}, fmt.Errorf("evaluating meta object: %w", err)
	}
	var m Meta
	if err := vm.ExportTo(v, &m); err != nil {
		return Meta{}, fmt.Errorf("decoding meta object: %w", err)
	}
	if m.Name == "" {
		return Meta{}, fmt.Errorf("workflow meta is missing a name")
	}
	return m, nil
}

// extractMetaLiteral finds `meta = { ... }` and returns the balanced object
// literal (brace-matched, string/comment aware).
func extractMetaLiteral(src string) (string, error) {
	loc := metaAssignRe.FindStringIndex(src)
	if loc == nil {
		return "", fmt.Errorf("no `meta = {...}` block found; a workflow must start with `export const meta = {...}`")
	}
	// loc[1] is just past the opening brace; back up to include it.
	open := loc[1] - 1
	end, err := matchBrace(src, open)
	if err != nil {
		return "", err
	}
	return src[open : end+1], nil
}

// matchBrace returns the index of the '}' that closes the '{' at src[start],
// skipping over string literals (', ", `) and comments (// and /* */).
func matchBrace(src string, start int) (int, error) {
	if start >= len(src) || src[start] != '{' {
		return 0, fmt.Errorf("internal: matchBrace not at '{'")
	}
	depth := 0
	for i := start; i < len(src); i++ {
		c := src[i]
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		case '"', '\'', '`':
			j, err := skipString(src, i, c)
			if err != nil {
				return 0, err
			}
			i = j
		case '/':
			if i+1 < len(src) {
				switch src[i+1] {
				case '/':
					i = skipLineComment(src, i)
				case '*':
					j, err := skipBlockComment(src, i)
					if err != nil {
						return 0, err
					}
					i = j
				}
			}
		}
	}
	return 0, fmt.Errorf("unbalanced braces in meta object")
}

func skipString(src string, i int, quote byte) (int, error) {
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++ // skip escaped char
		case quote:
			return j, nil
		}
	}
	return 0, fmt.Errorf("unterminated string in meta object")
}

func skipLineComment(src string, i int) int {
	for j := i; j < len(src); j++ {
		if src[j] == '\n' {
			return j
		}
	}
	return len(src) - 1
}

func skipBlockComment(src string, i int) (int, error) {
	for j := i + 2; j+1 < len(src); j++ {
		if src[j] == '*' && src[j+1] == '/' {
			return j + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated block comment in meta object")
}

// wrapSource turns a workflow source into a runnable program: the prelude, then
// the body wrapped in an async function (so top-level `await` and top-level
// `return` are legal), then a .then() that funnels the result/error back to Go via
// the __flowResolve / __flowReject host functions.
func wrapSource(src string) string {
	body := stripExports(src)
	var b strings.Builder
	b.WriteString(preludeJS)
	b.WriteString("\n;globalThis.__flowMain = async function () {\n")
	b.WriteString(body)
	b.WriteString("\n};\n")
	b.WriteString("__flowMain().then(function (v) { __flowResolve(v); }, function (e) { __flowReject(e && e.stack ? String(e.stack) : String(e)); });\n")
	return b.String()
}

// stripExports removes leading `export` (and `export default`) keywords so the
// body's `export const meta = ...` etc. become plain statements valid inside the
// async wrapper. Workflow scripts only use `export const meta`; other export forms
// are handled best-effort.
func stripExports(src string) string {
	src = exportDefaultRe.ReplaceAllString(src, "$1")
	src = exportRe.ReplaceAllString(src, "$1$2")
	return src
}
