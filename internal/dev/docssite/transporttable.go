package docssite

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gadget-inc/skipper/internal/router"
)

// errTransportBoolUnmapped is returned (wrapped) when
// HTTPTransportSettings carries a bool field with no entry in
// transportProseMap. The renderer fails the docs build instead of
// silently dropping the row.
var errTransportBoolUnmapped = errors.New("docssite: transport bool field has no prose mapping")

// transportProseRow describes the user-visible row produced by a
// single bool field of [router.HTTPTransportSettings]. Label is the
// table's Setting cell; trueText / falseText are the Value cells
// rendered when the field's value is true / false respectively.
type transportProseRow struct {
	Label     string
	TrueText  string
	FalseText string
}

// transportProseMap is the prose dictionary keyed by struct-field
// name. Adding a bool to [router.HTTPTransportSettings] without
// adding the corresponding entry here fails the docs build via
// errTransportBoolUnmapped.
var transportProseMap = map[string]transportProseRow{
	"ForceAttemptHTTP2": {
		Label:     "Protocol",
		TrueText:  "HTTP/2 attempted (falls back to HTTP/1.1 if unsupported)",
		FalseText: "HTTP/1.1 only",
	},
	"DisableCompression": {
		Label:     "Compression",
		TrueText:  "Disabled (no `Accept-Encoding` sent)",
		FalseText: "Enabled (default `Accept-Encoding` sent)",
	},
}

// renderTransportTable emits the markdown body of the router HTTP
// transport-settings table from the production
// [router.DefaultHTTPTransportSettings].
func renderTransportTable() (string, error) {
	return renderTransportRows(router.DefaultHTTPTransportSettings)
}

// renderTransportRows is the deterministic core of
// [renderTransportTable]. Numeric fields render in declaration
// order; bool fields render afterwards via the prose map.
func renderTransportRows(s router.HTTPTransportSettings) (string, error) {
	t := reflect.TypeOf(s)
	v := reflect.ValueOf(s)

	var boolFieldNames []string
	type numericRow struct{ label, value string }
	var numerics []numericRow

	for i := range t.NumField() {
		f := t.Field(i)
		fv := v.Field(i)
		switch f.Type.Kind() {
		case reflect.Bool:
			boolFieldNames = append(boolFieldNames, f.Name)
		default:
			label, val, ok := transportNumericRow(f, fv)
			if !ok {
				return "", fmt.Errorf("docssite: transport field %s has unsupported kind %s", f.Name, f.Type.Kind())
			}
			numerics = append(numerics, numericRow{label, val})
		}
	}

	if err := checkTransportProseMapCoverage(transportProseMap, boolFieldNames); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("| Setting | Value |\n")
	b.WriteString("| --- | --- |\n")
	for _, r := range numerics {
		fmt.Fprintf(&b, "| %s | %s |\n", escapePipes(r.label), escapePipes(r.value))
	}
	for _, name := range boolFieldNames {
		row := transportProseMap[name]
		fv := v.FieldByName(name)
		text := row.FalseText
		if fv.Bool() {
			text = row.TrueText
		}
		fmt.Fprintf(&b, "| %s | %s |\n", escapePipes(row.Label), escapePipes(text))
	}
	return b.String(), nil
}

// checkTransportProseMapCoverage returns errTransportBoolUnmapped
// (wrapped with the offending field name) when any boolFieldName is
// not present in m.
func checkTransportProseMapCoverage(m map[string]transportProseRow, boolFieldNames []string) error {
	for _, name := range boolFieldNames {
		if _, ok := m[name]; !ok {
			return fmt.Errorf("%w: %s", errTransportBoolUnmapped, name)
		}
	}
	return nil
}

// transportNumericRow renders one non-bool field as a (label,
// value) pair. Returns ok=false for kinds the renderer does not
// know how to format -- a guardrail against a future field type
// (slice, struct, ...) sneaking past the docs build.
//
// The duration check precedes the int-kind check because
// [time.Duration] reports [reflect.Int64] as its Kind; treating it
// as a plain int64 would render raw nanoseconds.
func transportNumericRow(f reflect.StructField, v reflect.Value) (label, value string, ok bool) {
	label = humanizeTransportFieldName(f.Name)
	if v.Type() == reflect.TypeFor[time.Duration]() {
		return label, time.Duration(v.Int()).String(), true
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return label, strconv.FormatInt(v.Int(), 10), true
	}
	return "", "", false
}

// humanizeTransportFieldName converts a Go struct-field name to
// the operator-facing label used in the table. The mapping is
// hand-curated -- "DialTimeout" reads as "Dial timeout," but
// "MaxIdleConns" is already a known term and stays compact.
func humanizeTransportFieldName(name string) string {
	switch name {
	case "DialTimeout":
		return "Dial timeout"
	case "KeepAlive":
		return "Keep-alive"
	case "MaxIdleConns":
		return "Max idle connections"
	case "IdleConnTimeout":
		return "Idle connection timeout"
	case "TLSHandshakeTimeout":
		return "TLS handshake timeout"
	}
	return name
}
