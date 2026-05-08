package docssite

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/pprof"
	"github.com/gadget-inc/skipper/internal/router"
	"github.com/gadget-inc/skipper/internal/telemetry"
)

// errUnknownFlagSection is returned (wrapped) when renderFlagTable is
// called with a section name not present in flagTableBindings.
var errUnknownFlagSection = errors.New("docssite: unknown flagtable section")

// flagTableBinding ties one section name in the configuration reference
// markdown to the config struct(s) whose `flag:"…"` tags populate that
// section's table. Order in Structs is the source of truth for row
// order across multiple structs in a single section.
type flagTableBinding struct {
	Section string
	Structs []any
}

// flagTableBindings is the registry consulted by renderFlagTable. The
// section names are slugs that match the {{< flagtable section >}}
// shortcode argument in docs/content/reference/configuration.md.
var flagTableBindings = []flagTableBinding{
	{Section: "controller", Structs: []any{controller.Config{}}},
	{Section: "router", Structs: []any{router.Config{}}},
	{Section: "shared", Structs: []any{log.Config{}, pprof.Config{}, telemetry.Config{}}},
}

// flagTypeRewrites maps a reflect.Type's String() form to a friendlier
// name used in the rendered Type column. Keys are matched on the
// nominal type string, so named types (LogLevel, PasetoPrivateKey) are
// rewritten to their underlying primitive rather than their Go type.
var flagTypeRewrites = map[string]string{
	"time.Duration":               "duration",
	"[]string":                    "string list",
	"log.LogLevel":                "string",
	"controller.PasetoPrivateKey": "string",
}

// renderFlagTable returns the markdown table body for the named
// section: a header row, a separator row, and one data row per
// `flag:"…"`-tagged field across the bound structs in declaration
// order. Returns errUnknownFlagSection (wrapped) if section is not
// bound. Malformed `required:"…"` or `sensitive:"…"` tag values
// surface as wrapped errors so dev docs build fails loudly.
func renderFlagTable(section string) (string, error) {
	for _, b := range flagTableBindings {
		if b.Section != section {
			continue
		}
		out, err := renderFlagRows(b.Structs)
		if err != nil {
			return "", fmt.Errorf("flagtable %q: %w", section, err)
		}
		return out, nil
	}
	return "", fmt.Errorf("flagtable %q: %w", section, errUnknownFlagSection)
}

// renderFlagRows walks each struct in declaration order and emits a
// markdown table whose data rows describe every `flag:"…"`-tagged
// field. Exposed (unexported) so tests can feed test-only structs.
func renderFlagRows(structs []any) (string, error) {
	var b strings.Builder
	b.WriteString("| Flag | Type | Default | Description |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, s := range structs {
		t := reflect.TypeOf(s)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return "", fmt.Errorf("expected struct, got %s", t.Kind())
		}
		for f := range t.Fields() {
			flag := f.Tag.Get("flag")
			if flag == "" {
				continue
			}
			canonical, _, _ := strings.Cut(flag, ",")
			canonical = strings.TrimSpace(canonical)
			row, err := renderFlagRow(f, canonical)
			if err != nil {
				return "", err
			}
			b.WriteString(row)
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

func renderFlagRow(f reflect.StructField, flag string) (string, error) {
	required, err := parseBoolTag(f.Tag.Get("required"))
	if err != nil {
		return "", fmt.Errorf("field %s: required tag: %w", f.Name, err)
	}
	sensitive, err := parseBoolTag(f.Tag.Get("sensitive"))
	if err != nil {
		return "", fmt.Errorf("field %s: sensitive tag: %w", f.Name, err)
	}

	defaultCell := composeDefault(f.Tag.Get("default"), required, sensitive)
	typeCell := flagTypeName(f.Type)
	desc := strings.TrimSpace(f.Tag.Get("description"))

	return fmt.Sprintf(
		"| `--%s` | %s | %s | %s |",
		flag,
		escapePipes(typeCell),
		escapePipes(defaultCell),
		escapePipes(desc),
	), nil
}

// composeDefault produces the Default-cell contents per the rules:
//   - required:"true" → "(required)"
//   - else default:"X" → "`X`"
//   - else empty
//   - sensitive:"true" → append ", sensitive"
func composeDefault(rawDefault string, required, sensitive bool) string {
	var s string
	switch {
	case required:
		s = "(required)"
	case rawDefault != "":
		s = "`" + rawDefault + "`"
	}
	if sensitive {
		if s == "" {
			s = ", sensitive"
		} else {
			s += ", sensitive"
		}
	}
	return s
}

// flagTypeName returns the user-facing type string for a struct field.
// Named types listed in flagTypeRewrites resolve to their friendly
// alias; everything else uses reflect.Type.String() unchanged.
func flagTypeName(t reflect.Type) string {
	name := t.String()
	if alias, ok := flagTypeRewrites[name]; ok {
		return alias
	}
	return name
}

// parseBoolTag accepts "" (false), "true" (true), or "false" (false).
// Anything else is rejected so a typo like `required:"yes"` fails the
// docs build instead of silently behaving as not-required.
func parseBoolTag(v string) (bool, error) {
	switch v {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, fmt.Errorf("invalid bool %q: want \"true\" or \"false\"", v)
}

// escapePipes escapes a literal `|` so it does not split a markdown
// table cell. Backslash-pipe is the GFM-recognized form.
func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
