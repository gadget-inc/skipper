package flag

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Flag is a wrapper around a cobra flag that provides additional functionality.
type Flag[T any] struct {
	// Name is the name of the flag.
	//
	// This is used as the double-dashed flag name in the command line
	// and as the name of the environment variable. The name of the
	// environment variable is derived by upper-casing, replacing
	// hyphens with underscores, and prefixing with "FUSION_". For
	// example, the name "foo-bar" would result in a flag named
	// --foo-bar and an environment variable named FUSION_FOO_BAR.
	Name string

	// EnvVarName is the name of the environment variable.
	//
	// This is used to override the default environment variable name.
	EnvVarName string

	// Shorthand is the shorthand name of the flag.
	//
	// This is used as the single-dash flag name in the command line. If
	// not set, no shorthand is used.
	Shorthand string

	// Description is the description of the flag.
	//
	// This is used in the help text to describe the flag and its usage.
	Description string

	// Default is the default value of the flag.
	//
	// This is used when the flag is not set via the command line or
	// environment variable.
	Default T

	// Required is used to indicate that the flag is required.
	//
	// If true and the flag is not set via the command line or
	// environment variable, the bound command will return an error
	// before Run/RunE is called.
	Required bool

	// Separator is the separator to use when parsing the string
	// representation of the flag's value.
	//
	// This is only used when the flag's type is a slice or map. By
	// default, the value is split by commas.
	Separator string

	// Sensitive indicates whether the flag's value is sensitive.
	//
	// Use this to redact the flags value from help text, error
	// messages, and logs.
	Sensitive bool

	// Parse is used to parse the string representation of the flag's
	// value.
	//
	// This is used to convert the command line or environment
	// variable's value into the flag's underlying type. Use this to
	// implement custom parsing or to override the default parser for a
	// given type.
	Parse func(string) (T, error)

	// Display is used to display the flag's value in the help text.
	//
	// This is used when displaying the default value. Use this to
	// implement a custom display.
	Display func(T) string

	// Action is called immediately after the flag has been set.
	//
	// Use this to perform validation or other actions that depend on
	// the flag value.
	Action func(T) error

	// WasProvided indicates whether the flag was set via the command line or
	// environment variable.
	//
	// This is useful for determining whether the flag was set to its
	// default value or not.
	WasProvided bool

	// Value is the underlying value of the flag.
	//
	// This is where the value is stored after the flag has been set.
	value T

	// ptr is a pointer to the flag's ptr.
	//
	// This is used to perform switch.(type) checks which aren't
	// possible with generic types.
	ptr any

	isInitialized bool
}

var _ pflag.Value = (*Flag[any])(nil)

// Init initializes the flag.
//
// This sets the flag's value to the default value and sets the
// environment variable name if it is not set.
//
// This is called automatically by Bind/BindPersistent and is only
// needed when you want to access the flag's value without binding it to
// a command (e.g. in a test).
func (self *Flag[T]) Init() {
	if self.isInitialized {
		return
	}

	self.ptr = any(&self.value)
	self.value = self.Default
	if self.EnvVarName == "" {
		self.EnvVarName = "FUSION_" + strings.ToUpper(strings.ReplaceAll(self.Name, "-", "_"))
	}
	self.Description += " (env " + self.EnvVarName + ")"
	self.isInitialized = true
}

// Value returns the flag's value.
//
// If the flag was not set or initialized via Bind/BindPersistent,
// this will panic.
func (self *Flag[T]) Value() T {
	if !self.isInitialized {
		panic(fmt.Sprintf("flag --%s was not initialized", self.Name))
	}
	return self.value
}

// SetValue sets the flag's value.
func (self *Flag[T]) SetValue(value T) error {
	self.Init()
	self.WasProvided = true
	self.value = value
	self.ptr = any(&self.value)
	if self.Action != nil {
		return self.Action(value)
	}
	return nil
}

// Set implements pflag.Value.Set.
func (self *Flag[T]) Set(s string) error {
	self.Init()
	self.WasProvided = true

	var err error
	if self.Parse != nil {
		self.value, err = self.Parse(s)
		if err == nil && self.Action != nil {
			err = self.Action(self.value)
		}
		return err
	}

	sep := self.Separator
	if sep == "" {
		sep = ","
	}

	switch value := self.ptr.(type) {
	case *string:
		*value = s
	case *[]string:
		ss := strings.Split(s, sep)
		*value = make([]string, len(ss))
		for i, s := range ss {
			(*value)[i] = strings.TrimSpace(s)
		}
	case *map[string]string:
		ss := strings.Split(s, sep)
		*value = make(map[string]string, len(ss))
		for _, s := range ss {
			s = strings.TrimSpace(s)
			parts := strings.SplitN(s, "=", 2)
			if len(parts) != 2 {
				err = fmt.Errorf("invalid key=value pair: %s", s)
				break
			}
			(*value)[parts[0]] = parts[1]
		}
	case *int:
		*value, err = strconv.Atoi(s)
	case *int64:
		*value, err = strconv.ParseInt(s, 10, 64)
	case *uint64:
		*value, err = strconv.ParseUint(s, 10, 64)
	case *bool:
		*value, err = strconv.ParseBool(s)
	case *time.Duration:
		*value, err = time.ParseDuration(s)
	case *url.URL:
		u, e := url.Parse(s)
		*value, err = *u, e
	default:
		panic(fmt.Sprintf("unsupported type %T", value))
	}

	if err != nil {
		return fmt.Errorf("invalid value for flag --%s: %w", self.Name, err)
	}

	if self.Action != nil {
		err = self.Action(self.value)
	}

	return err
}

// String implements pflag.Value.String.
func (self *Flag[T]) String() string {
	if self.ptr == nil {
		return ""
	}

	if self.Display != nil {
		return self.Display(self.value)
	}

	if stringer, ok := self.ptr.(fmt.Stringer); ok {
		return stringer.String()
	}

	sep := self.Separator
	if sep == "" {
		sep = ","
	}
	if sep != " " {
		// turn "a,b" into "a, b"
		sep = sep + " "
	}

	switch value := self.ptr.(type) {
	case *string:
		return *value
	case *[]string:
		if len(*value) == 0 {
			return ""
		}
		return "[" + strings.Join(*value, sep) + "]"
	case *map[string]string:
		if len(*value) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString("[")
		for k, v := range *value {
			if b.Len() > 1 {
				b.WriteString(sep)
			}
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(v)
		}
		b.WriteString("]")
		return b.String()
	case *int:
		return strconv.Itoa(*value)
	case *int64:
		return strconv.FormatInt(*value, 10)
	case *uint64:
		return strconv.FormatUint(*value, 10)
	case *bool:
		return strconv.FormatBool(*value)
	default:
		panic(fmt.Sprintf("unsupported type %T", value))
	}
}

// Type implements pflag.Value.Type.
func (self *Flag[T]) Type() string {
	return ""
}

// Bind binds the flag to the given command.
//
// Once the command is executed and Run/RunE is called, the flag's value
// will be set to the value provided by the command line, environment
// variable, or default value.
func (self *Flag[T]) Bind(cmd *cobra.Command) {
	self.bind(cmd, false)
}

// Bind binds the flag to the given command.
//
// Once the command is executed and Run/RunE is called, the flag's value
// will be set to the value provided by the command line, environment
// variable, or default value.
func (self *Flag[T]) BindPersistent(cmd *cobra.Command) {
	self.bind(cmd, true)
}

func (self *Flag[T]) bind(cmd *cobra.Command, persistent bool) {
	self.Init()

	if persistent {
		cmd.PersistentFlags().VarP(self, self.Name, self.Shorthand, self.Description)
	} else {
		cmd.Flags().VarP(self, self.Name, self.Shorthand, self.Description)
	}

	// FIXME: we can't use MarkFlagRequired or MarkFlagsRequiredTogether because they don't know about environment variables but we want these when we generate our completion scripts
	// if self.Required {
	//  _ = cmd.MarkFlagRequired(self.Name)
	// }

	// if len(self.Requires) > 0 {
	// 	cmd.MarkFlagsRequiredTogether(append(self.Requires, self.Name)...)
	// }

	var nextPreRunE func(cmd *cobra.Command, args []string) error
	if persistent {
		nextPreRunE = cmd.PersistentPreRunE
	} else {
		nextPreRunE = cmd.PreRunE
	}

	preRunE := func(cmd *cobra.Command, args []string) error {
		if !self.WasProvided {
			// the flag wasn't set from the command line, check the environment
			envValue, ok := os.LookupEnv(self.EnvVarName)
			if ok {
				err := self.Set(envValue)
				if err != nil {
					return fmt.Errorf("error parsing environment variable %s: %w", self.EnvVarName, err)
				}
			}
		}

		if !self.WasProvided {
			// the flag wasn't set from the command line or environment
			if self.Required {
				return fmt.Errorf("flag --%s is required", self.Name)
			}

			if self.Action != nil {
				// the flag wasn't set, so the action was never called, call it now with the default value
				err := self.Action(self.value)
				if err != nil {
					return err
				}
			}
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
