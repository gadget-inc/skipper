package flag

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

var _ pflag.Value = (*Flag[any])(nil)

// Set implements pflag.Value.Set.
func (self *Flag[T]) Set(s string) error {
	self.WasSet = true

	var err error
	if self.Parse != nil {
		self.Value, err = self.Parse(s)
		if err == nil && self.Action != nil {
			err = self.Action(self.Value)
		}
		return err
	}

	sep := self.Separator
	if sep == "" {
		sep = ","
	}

	switch value := self.value.(type) {
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
		err = self.Action(self.Value)
	}

	return err
}

// String implements pflag.Value.String.
func (self *Flag[T]) String() string {
	if self.value == nil {
		return ""
	}

	if self.Display != nil {
		return self.Display(self.Value)
	}

	if stringer, ok := self.value.(fmt.Stringer); ok {
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

	switch value := self.value.(type) {
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
