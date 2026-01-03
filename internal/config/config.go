// Package config provides declarative configuration binding for [cobra.Command]
// using struct tags. It eliminates boilerplate by automatically registering CLI
// flags, setting up environment variable fallbacks, applying defaults, and
// validating required fields.
//
// # Struct Tags
//
// Configuration structs use the following tags to control flag behavior:
//
//   - flag: Flag name (required to register the field as a flag)
//   - description: Help text shown in --help output
//   - default: Default value, parsed from string representation
//   - required: If "true", command fails when flag is not provided
//   - sensitive: If "true", value is masked as "********" in help output
//   - separator: Delimiter for slice/map values (default ",")
//
// # Environment Variables
//
// Each flag automatically falls back to an environment variable when not
// provided on the command line. The variable name is derived from the flag
// name with a SKIPPER_ prefix:
//
//	--my-flag  →  SKIPPER_MY_FLAG
//
// Flags take precedence over environment variables.
//
// # Supported Types
//
// The following field types are supported:
//
//   - string, []string
//   - int, int32, int64, []int, []int32, []int64
//   - uint64, []uint64
//   - float32, float64, []float32, []float64
//   - bool, []bool
//   - time.Duration, []time.Duration
//   - url.URL
//   - map[string]string
//   - Any type implementing [encoding.TextUnmarshaler]
//
// # Example
//
//	type Config struct {
//	    Host    string        `flag:"host" default:"localhost" description:"Server host"`
//	    Port    int           `flag:"port" default:"8080" description:"Server port"`
//	    Timeout time.Duration `flag:"timeout" default:"30s" description:"Request timeout"`
//	    APIKey  string        `flag:"api-key" required:"true" sensitive:"true"`
//	    Tags    []string      `flag:"tags" separator:"," description:"Comma-separated tags"`
//	}
//
//	func main() {
//	    cfg := config.New[Config]()
//	    cmd := &cobra.Command{Use: "server", RunE: func(*cobra.Command, []string) error {
//	        fmt.Printf("Starting server on %s:%d\n", cfg.Host, cfg.Port)
//	        return nil
//	    }}
//	    config.Bind(cmd, cfg)
//	    cmd.Execute()
//	}
package config

import (
	"encoding"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Bind registers a config struct's fields as flags on the given command.
// Fields without a "flag" tag are ignored. The config must be a pointer to a
// struct. See package documentation for supported struct tags and field types.
func Bind(cmd *cobra.Command, cfg any) {
	bind(cmd, cfg, false)
}

// BindPersistent registers a config struct's fields as persistent flags on the
// given command. Persistent flags are inherited by all subcommands, making this
// useful for shared configuration like logging or authentication options.
func BindPersistent(cmd *cobra.Command, cfg any) {
	bind(cmd, cfg, true)
}

// New allocates a new config struct and applies default values from its
// "default" struct tags. This is the recommended way to instantiate config
// structs before binding them to a command:
//
//	cfg := config.New[Config]()
//	config.Bind(cmd, cfg)
func New[T any]() *T {
	cfg := new(T)
	Default(cfg)
	return cfg
}

// Default populates a config struct with values from its "default" struct tags.
// This is useful when you need to apply defaults to an existing struct, such as
// in tests. For new structs, prefer [New]. The config must be a pointer to a
// struct; Default panics otherwise.
func Default(cfg any) {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		panic("config must be a pointer to a struct")
	}

	v = v.Elem()
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		fieldValue := v.Field(i)

		defaultStr := field.Tag.Get("default")
		if defaultStr == "" {
			continue
		}

		separator := field.Tag.Get("separator")
		if separator == "" {
			separator = ","
		}

		fv := &flagValue{
			value:     fieldValue.Addr().Interface(),
			separator: separator,
		}

		if err := fv.Set(defaultStr); err != nil {
			panic(fmt.Sprintf("invalid default value for field %s: %v", field.Name, err))
		}
	}
}

func bind(cmd *cobra.Command, cfg any, persistent bool) {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		panic("config must be a pointer to a struct")
	}

	v = v.Elem()
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		fieldValue := v.Field(i)

		flagName := field.Tag.Get("flag")
		if flagName == "" {
			continue // skip fields without flag tag
		}

		description := field.Tag.Get("description")
		required := field.Tag.Get("required") == "true"
		sensitive := field.Tag.Get("sensitive") == "true"
		separator := field.Tag.Get("separator")
		if separator == "" {
			separator = ","
		}

		// Build env var name from flag name
		envVarName := "SKIPPER_" + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
		description += " (env " + envVarName + ")"

		// Create the value wrapper
		fv := &flagValue{
			value:     fieldValue.Addr().Interface(),
			sensitive: sensitive,
			separator: separator,
		}

		// Register the flag
		var flags *pflag.FlagSet
		if persistent {
			flags = cmd.PersistentFlags()
		} else {
			flags = cmd.Flags()
		}

		flag := flags.VarPF(fv, flagName, "", description)
		if fv.IsBoolFlag() {
			flag.NoOptDefVal = "true"
		}

		// Add PreRunE hook for env var fallback and required validation
		addPreRun(cmd, persistent, flagName, envVarName, required, fv)
	}
}

func addPreRun(cmd *cobra.Command, persistent bool, flagName, envVarName string, required bool, fv *flagValue) {
	var nextPreRunE func(cmd *cobra.Command, args []string) error
	if persistent {
		nextPreRunE = cmd.PersistentPreRunE
	} else {
		nextPreRunE = cmd.PreRunE
	}

	preRunE := func(cmd *cobra.Command, args []string) error {
		if !fv.wasProvided {
			// Check environment variable
			if envValue, ok := os.LookupEnv(envVarName); ok {
				if err := fv.Set(envValue); err != nil {
					return fmt.Errorf("error parsing environment variable %s: %w", envVarName, err)
				}
			}
		}

		if !fv.wasProvided && required {
			return fmt.Errorf("flag --%s is required", flagName)
		}

		if nextPreRunE != nil {
			return nextPreRunE(cmd, args)
		}
		return nil
	}

	if persistent {
		cmd.PersistentPreRunE = preRunE
	} else {
		cmd.PreRunE = preRunE
	}
}

// parseSlice parses a separator-delimited string into a slice using the provided parse function.
func parseSlice[T any](s, sep string, parse func(string) (T, error)) ([]T, error) {
	parts := strings.Split(s, sep)
	result := make([]T, len(parts))
	for i, part := range parts {
		v, err := parse(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

// formatSlice formats a slice into a bracketed string using the provided format function.
func formatSlice[T any](slice []T, sep string, format func(T) string) string {
	if len(slice) == 0 {
		return ""
	}
	parts := make([]string, len(slice))
	for i, v := range slice {
		parts[i] = format(v)
	}
	return "[" + strings.Join(parts, sep) + "]"
}

// formatMap formats a map into a bracketed string of key=value pairs.
func formatMap(m map[string]string, sep string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return "[" + strings.Join(parts, sep) + "]"
}

// flagValue wraps a config field value and implements pflag.Value.
type flagValue struct {
	value       any
	sensitive   bool
	separator   string
	wasProvided bool
}

var _ pflag.Value = (*flagValue)(nil)

func (fv *flagValue) Set(s string) error {
	fv.wasProvided = true

	// Check if the value implements encoding.TextUnmarshaler
	if tu, ok := fv.value.(encoding.TextUnmarshaler); ok {
		return tu.UnmarshalText([]byte(s))
	}

	switch v := fv.value.(type) {
	case *string:
		*v = s
	case *[]string:
		parts := strings.Split(s, fv.separator)
		*v = make([]string, len(parts))
		for i, part := range parts {
			(*v)[i] = strings.TrimSpace(part)
		}
	case *int:
		i, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*v = i
	case *[]int:
		result, err := parseSlice(s, fv.separator, strconv.Atoi)
		if err != nil {
			return err
		}
		*v = result
	case *int32:
		i, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return err
		}
		*v = int32(i)
	case *[]int32:
		result, err := parseSlice(s, fv.separator, func(s string) (int32, error) {
			n, err := strconv.ParseInt(s, 10, 32)
			return int32(n), err
		})
		if err != nil {
			return err
		}
		*v = result
	case *int64:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*v = i
	case *[]int64:
		result, err := parseSlice(s, fv.separator, func(s string) (int64, error) {
			return strconv.ParseInt(s, 10, 64)
		})
		if err != nil {
			return err
		}
		*v = result
	case *uint64:
		i, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		*v = i
	case *[]uint64:
		result, err := parseSlice(s, fv.separator, func(s string) (uint64, error) {
			return strconv.ParseUint(s, 10, 64)
		})
		if err != nil {
			return err
		}
		*v = result
	case *float32:
		f, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return err
		}
		*v = float32(f)
	case *[]float32:
		result, err := parseSlice(s, fv.separator, func(s string) (float32, error) {
			f, err := strconv.ParseFloat(s, 32)
			return float32(f), err
		})
		if err != nil {
			return err
		}
		*v = result
	case *float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*v = f
	case *[]float64:
		result, err := parseSlice(s, fv.separator, func(s string) (float64, error) {
			return strconv.ParseFloat(s, 64)
		})
		if err != nil {
			return err
		}
		*v = result
	case *bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		*v = b
	case *[]bool:
		result, err := parseSlice(s, fv.separator, strconv.ParseBool)
		if err != nil {
			return err
		}
		*v = result
	case *time.Duration:
		d, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*v = d
	case *[]time.Duration:
		result, err := parseSlice(s, fv.separator, time.ParseDuration)
		if err != nil {
			return err
		}
		*v = result
	case *url.URL:
		u, err := url.Parse(s)
		if err != nil {
			return err
		}
		*v = *u
	case *map[string]string:
		result, err := parseSlice(s, fv.separator, func(s string) (struct{ k, v string }, error) {
			kv := strings.SplitN(s, "=", 2)
			if len(kv) != 2 {
				return struct{ k, v string }{}, fmt.Errorf("invalid key=value pair: %s", s)
			}
			return struct{ k, v string }{kv[0], kv[1]}, nil
		})
		if err != nil {
			return err
		}
		*v = make(map[string]string, len(result))
		for _, kv := range result {
			(*v)[kv.k] = kv.v
		}
	default:
		return fmt.Errorf("unsupported type %T", fv.value)
	}
	return nil
}

func (fv *flagValue) String() string {
	if fv.sensitive {
		return "********"
	}

	if fv.value == nil {
		return ""
	}

	// Check if the value implements encoding.TextMarshaler or fmt.Stringer
	if tm, ok := fv.value.(encoding.TextMarshaler); ok {
		text, err := tm.MarshalText()
		if err == nil {
			return string(text)
		}
	}
	if s, ok := fv.value.(fmt.Stringer); ok {
		return s.String()
	}

	// Add space after separator for readability (e.g., "," becomes ", ")
	sep := fv.separator
	if sep == "" {
		sep = ","
	}
	if sep != " " {
		sep += " "
	}

	switch v := fv.value.(type) {
	case *string:
		return *v
	case *[]string:
		return formatSlice(*v, sep, func(s string) string { return s })
	case *int:
		return strconv.Itoa(*v)
	case *[]int:
		return formatSlice(*v, sep, strconv.Itoa)
	case *int32:
		return strconv.Itoa(int(*v))
	case *[]int32:
		return formatSlice(*v, sep, func(n int32) string { return strconv.Itoa(int(n)) })
	case *int64:
		return strconv.FormatInt(*v, 10)
	case *[]int64:
		return formatSlice(*v, sep, func(n int64) string { return strconv.FormatInt(n, 10) })
	case *uint64:
		return strconv.FormatUint(*v, 10)
	case *[]uint64:
		return formatSlice(*v, sep, func(n uint64) string { return strconv.FormatUint(n, 10) })
	case *float32:
		return strconv.FormatFloat(float64(*v), 'f', -1, 32)
	case *[]float32:
		return formatSlice(*v, sep, func(f float32) string { return strconv.FormatFloat(float64(f), 'f', -1, 32) })
	case *float64:
		return strconv.FormatFloat(*v, 'f', -1, 64)
	case *[]float64:
		return formatSlice(*v, sep, func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) })
	case *bool:
		return strconv.FormatBool(*v)
	case *[]bool:
		return formatSlice(*v, sep, strconv.FormatBool)
	case *time.Duration:
		return v.String()
	case *[]time.Duration:
		return formatSlice(*v, sep, func(d time.Duration) string { return d.String() })
	case *url.URL:
		return v.String()
	case *map[string]string:
		return formatMap(*v, sep)
	default:
		return fmt.Sprintf("%v", fv.value)
	}
}

func (fv *flagValue) Type() string {
	switch fv.value.(type) {
	case *string:
		return "string"
	case *[]string:
		return "strings"
	case *int, *int32, *int64:
		return "int"
	case *[]int, *[]int32, *[]int64:
		return "ints"
	case *uint64:
		return "uint"
	case *[]uint64:
		return "uints"
	case *float32, *float64:
		return "float"
	case *[]float32, *[]float64:
		return "floats"
	case *bool:
		return "bool"
	case *[]bool:
		return "bools"
	case *time.Duration:
		return "duration"
	case *[]time.Duration:
		return "durations"
	case *url.URL:
		return "url"
	case *map[string]string:
		return "map"
	default:
		return ""
	}
}

func (fv *flagValue) IsBoolFlag() bool {
	_, ok := fv.value.(*bool)
	return ok
}
