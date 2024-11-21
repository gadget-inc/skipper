package flag

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Flag is a wrapper around a cobra flag that provides additional functionality.
type Flag[T any] struct {
	// Name is the name of the flag.
	//
	// This is used as the double-dashed flag name in the command line and as the name of the environment variable. The
	// name of the environment variable is derived by upper-casing, replacing hyphens with underscores, and prefixing
	// with "FUSION_". For example, the name "foo-bar" would result in the environment variable "FUSION_FOO_BAR".
	Name string

	// Shorthand is the shorthand name of the flag.
	//
	// This is used as the single-dash flag name in the command line. If not set, no shorthand is used.
	Shorthand string

	// Description is the description of the flag.
	//
	// This is used in the help text to describe the flag and its usage.
	Description string

	// Default is the default value of the flag.
	//
	// This is used when the flag is not set via the command line or environment variable.
	Default T

	// Required is used to indicate that the flag is required.
	//
	// If true and the flag is not set via the command line or environment variable, the bound command will return an
	// error before Run/RunE is called.
	Required bool

	// Requires is a list of flags that this flag depends on.
	//
	// If any of the flags in this list are not set, the bound command will return an error before Run/RunE is called.
	Requires []string

	// Separator is the separator to use when parsing the string representation of the flag's value.
	//
	// This is only used when the flag's type is a slice or map. By default, the value is split by commas.
	Separator string

	// Sensitive indicates whether the flag's value is sensitive.
	//
	// Use this to redact the flags value from help text, error messages, and logs.
	Sensitive bool

	// Parse is used to parse the string representation of the flag's value.
	//
	// This is used to convert the command line or environment variable's value into the flag's underlying type. Use
	// this to implement custom parsing or to override the default parser for a given type.
	Parse func(string) (T, error)

	// Display is used to display the flag's value in the help text.
	//
	// This is used when displaying the default value. Use this to implement a custom display.
	Display func(T) string

	// Action is called immediately after the flag has been set.
	//
	// Use this to perform validation or other actions that depend on the flag value.
	Action func(T) error

	// Value is the underlying value of the flag.
	//
	// This is where the value is stored after the flag has been set.
	Value T

	// WasSet indicates whether the flag was set via the command line or environment variable.
	//
	// This is useful for determining whether the flag was set to its default value or not.
	WasSet bool

	// value is a pointer to the flag's value.
	//
	// This is used to perform switch.(type) checks which aren't possible with generic types.
	value any
}

// Bind binds the flag to the given command.
//
// Once the command is executed and Run/RunE is called, the flag's value will be set to the value provided by the
// command line, environment variable, or default value.
func (self *Flag[T]) Bind(cmd *cobra.Command) {
	self.bind(cmd, false)
}

// Bind binds the flag to the given command.
//
// Once the command is executed and Run/RunE is called, the flag's value will be set to the value provided by the
// command line, environment variable, or default value.
func (self *Flag[T]) BindPersistent(cmd *cobra.Command) {
	self.bind(cmd, true)
}

func (self *Flag[T]) bind(cmd *cobra.Command, persistent bool) {
	self.value = any(&self.Value)

	// set the default value now so that it is displayed in the help text
	self.Value = self.Default

	envName := "FUSION_" + strings.ToUpper(strings.ReplaceAll(self.Name, "-", "_"))
	self.Description += " (env " + envName + ")"

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
		if !self.WasSet {
			// the flag wasn't set from the command line, check the environment
			envValue, ok := os.LookupEnv(envName)
			if ok {
				err := self.Set(envValue)
				if err != nil {
					return fmt.Errorf("error parsing environment variable %s: %w", envName, err)
				}
			}
		}

		if !self.WasSet {
			// the flag wasn't set from the command line or environment
			if self.Required {
				return fmt.Errorf("flag --%s is required", self.Name)
			}

			if self.Action != nil {
				// the flag wasn't set, so the action was never called, call it now with the default value
				err := self.Action(self.Value)
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
