package config

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"gotest.tools/v3/assert"
)

// testTextUnmarshaler is a custom type implementing encoding.TextUnmarshaler for testing.
type testTextUnmarshaler struct {
	Value string
}

func (t *testTextUnmarshaler) UnmarshalText(text []byte) error {
	t.Value = "unmarshaled:" + string(text)
	return nil
}

func (t *testTextUnmarshaler) String() string {
	return t.Value
}

// TestFlagValue_Set tests the Set method for all supported types.
func TestFlagValue_Set(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var s string
		fv := &flagValue{value: &s}

		err := fv.Set("hello")
		assert.NilError(t, err)
		assert.Equal(t, s, "hello")
		assert.Assert(t, fv.wasProvided)
	})

	t.Run("string slice with default separator", func(t *testing.T) {
		var ss []string
		fv := &flagValue{value: &ss, separator: ","}

		err := fv.Set("a, b, c")
		assert.NilError(t, err)
		assert.DeepEqual(t, ss, []string{"a", "b", "c"})
	})

	t.Run("string slice with custom separator", func(t *testing.T) {
		var ss []string
		fv := &flagValue{value: &ss, separator: ";"}

		err := fv.Set("a; b; c")
		assert.NilError(t, err)
		assert.DeepEqual(t, ss, []string{"a", "b", "c"})
	})

	t.Run("int slice", func(t *testing.T) {
		var is []int
		fv := &flagValue{value: &is, separator: ","}

		err := fv.Set("1, 2, 3")
		assert.NilError(t, err)
		assert.DeepEqual(t, is, []int{1, 2, 3})
	})

	t.Run("int slice invalid", func(t *testing.T) {
		var is []int
		fv := &flagValue{value: &is, separator: ","}

		err := fv.Set("1, not-a-number, 3")
		assert.Assert(t, err != nil)
	})

	t.Run("int32 slice", func(t *testing.T) {
		var is []int32
		fv := &flagValue{value: &is, separator: ","}

		err := fv.Set("1, 2, 3")
		assert.NilError(t, err)
		assert.DeepEqual(t, is, []int32{1, 2, 3})
	})

	t.Run("int32 slice invalid", func(t *testing.T) {
		var is []int32
		fv := &flagValue{value: &is, separator: ","}

		err := fv.Set("1, not-a-number, 3")
		assert.Assert(t, err != nil)
	})

	t.Run("int64 slice", func(t *testing.T) {
		var is []int64
		fv := &flagValue{value: &is, separator: ","}

		err := fv.Set("1, 2, 9223372036854775807")
		assert.NilError(t, err)
		assert.DeepEqual(t, is, []int64{1, 2, 9223372036854775807})
	})

	t.Run("int64 slice invalid", func(t *testing.T) {
		var is []int64
		fv := &flagValue{value: &is, separator: ","}

		err := fv.Set("1, not-a-number, 3")
		assert.Assert(t, err != nil)
	})

	t.Run("uint64 slice", func(t *testing.T) {
		var us []uint64
		fv := &flagValue{value: &us, separator: ","}

		err := fv.Set("1, 2, 18446744073709551615")
		assert.NilError(t, err)
		assert.DeepEqual(t, us, []uint64{1, 2, 18446744073709551615})
	})

	t.Run("uint64 slice invalid", func(t *testing.T) {
		var us []uint64
		fv := &flagValue{value: &us, separator: ","}

		err := fv.Set("1, not-a-number, 3")
		assert.Assert(t, err != nil)
	})

	t.Run("float32 slice", func(t *testing.T) {
		var fs []float32
		fv := &flagValue{value: &fs, separator: ","}

		err := fv.Set("1.1, 2.2, 3.3")
		assert.NilError(t, err)
		assert.DeepEqual(t, fs, []float32{1.1, 2.2, 3.3})
	})

	t.Run("float32 slice invalid", func(t *testing.T) {
		var fs []float32
		fv := &flagValue{value: &fs, separator: ","}

		err := fv.Set("1.1, not-a-number, 3.3")
		assert.Assert(t, err != nil)
	})

	t.Run("float64 slice", func(t *testing.T) {
		var fs []float64
		fv := &flagValue{value: &fs, separator: ","}

		err := fv.Set("1.1, 2.2, 3.14159265359")
		assert.NilError(t, err)
		assert.DeepEqual(t, fs, []float64{1.1, 2.2, 3.14159265359})
	})

	t.Run("float64 slice invalid", func(t *testing.T) {
		var fs []float64
		fv := &flagValue{value: &fs, separator: ","}

		err := fv.Set("1.1, not-a-number, 3.3")
		assert.Assert(t, err != nil)
	})

	t.Run("bool slice", func(t *testing.T) {
		var bs []bool
		fv := &flagValue{value: &bs, separator: ","}

		err := fv.Set("true, false, true")
		assert.NilError(t, err)
		assert.DeepEqual(t, bs, []bool{true, false, true})
	})

	t.Run("bool slice invalid", func(t *testing.T) {
		var bs []bool
		fv := &flagValue{value: &bs, separator: ","}

		err := fv.Set("true, maybe, false")
		assert.Assert(t, err != nil)
	})

	t.Run("duration slice", func(t *testing.T) {
		var ds []time.Duration
		fv := &flagValue{value: &ds, separator: ","}

		err := fv.Set("1s, 2m, 3h")
		assert.NilError(t, err)
		assert.DeepEqual(t, ds, []time.Duration{time.Second, 2 * time.Minute, 3 * time.Hour})
	})

	t.Run("duration slice invalid", func(t *testing.T) {
		var ds []time.Duration
		fv := &flagValue{value: &ds, separator: ","}

		err := fv.Set("1s, not-a-duration, 3h")
		assert.Assert(t, err != nil)
	})

	t.Run("map string string with default separator", func(t *testing.T) {
		var m map[string]string
		fv := &flagValue{value: &m, separator: ","}

		err := fv.Set("key1=val1, key2=val2")
		assert.NilError(t, err)
		assert.DeepEqual(t, m, map[string]string{"key1": "val1", "key2": "val2"})
	})

	t.Run("map string string with custom separator", func(t *testing.T) {
		var m map[string]string
		fv := &flagValue{value: &m, separator: ";"}

		err := fv.Set("key1=val1; key2=val2")
		assert.NilError(t, err)
		assert.DeepEqual(t, m, map[string]string{"key1": "val1", "key2": "val2"})
	})

	t.Run("map string string invalid format", func(t *testing.T) {
		var m map[string]string
		fv := &flagValue{value: &m, separator: ","}

		err := fv.Set("invalid")
		assert.ErrorContains(t, err, "invalid key=value pair")
	})

	t.Run("int", func(t *testing.T) {
		var i int
		fv := &flagValue{value: &i}

		err := fv.Set("42")
		assert.NilError(t, err)
		assert.Equal(t, i, 42)
	})

	t.Run("int invalid", func(t *testing.T) {
		var i int
		fv := &flagValue{value: &i}

		err := fv.Set("not-a-number")
		assert.Assert(t, err != nil)
	})

	t.Run("int32", func(t *testing.T) {
		var i int32
		fv := &flagValue{value: &i}

		err := fv.Set("42")
		assert.NilError(t, err)
		assert.Equal(t, i, int32(42))
	})

	t.Run("int32 invalid", func(t *testing.T) {
		var i int32
		fv := &flagValue{value: &i}

		err := fv.Set("not-a-number")
		assert.Assert(t, err != nil)
	})

	t.Run("int64", func(t *testing.T) {
		var i int64
		fv := &flagValue{value: &i}

		err := fv.Set("9223372036854775807")
		assert.NilError(t, err)
		assert.Equal(t, i, int64(9223372036854775807))
	})

	t.Run("int64 invalid", func(t *testing.T) {
		var i int64
		fv := &flagValue{value: &i}

		err := fv.Set("not-a-number")
		assert.Assert(t, err != nil)
	})

	t.Run("uint64", func(t *testing.T) {
		var u uint64
		fv := &flagValue{value: &u}

		err := fv.Set("18446744073709551615")
		assert.NilError(t, err)
		assert.Equal(t, u, uint64(18446744073709551615))
	})

	t.Run("uint64 invalid", func(t *testing.T) {
		var u uint64
		fv := &flagValue{value: &u}

		err := fv.Set("not-a-number")
		assert.Assert(t, err != nil)
	})

	t.Run("float32", func(t *testing.T) {
		var f float32
		fv := &flagValue{value: &f}

		err := fv.Set("3.14")
		assert.NilError(t, err)
		assert.Equal(t, f, float32(3.14))
	})

	t.Run("float32 invalid", func(t *testing.T) {
		var f float32
		fv := &flagValue{value: &f}

		err := fv.Set("not-a-number")
		assert.Assert(t, err != nil)
	})

	t.Run("float64", func(t *testing.T) {
		var f float64
		fv := &flagValue{value: &f}

		err := fv.Set("3.14159265359")
		assert.NilError(t, err)
		assert.Equal(t, f, 3.14159265359)
	})

	t.Run("float64 invalid", func(t *testing.T) {
		var f float64
		fv := &flagValue{value: &f}

		err := fv.Set("not-a-number")
		assert.Assert(t, err != nil)
	})

	t.Run("bool true", func(t *testing.T) {
		var b bool
		fv := &flagValue{value: &b}

		err := fv.Set("true")
		assert.NilError(t, err)
		assert.Equal(t, b, true)
	})

	t.Run("bool false", func(t *testing.T) {
		var b bool
		fv := &flagValue{value: &b}

		err := fv.Set("false")
		assert.NilError(t, err)
		assert.Equal(t, b, false)
	})

	t.Run("bool invalid", func(t *testing.T) {
		var b bool
		fv := &flagValue{value: &b}

		err := fv.Set("maybe")
		assert.Assert(t, err != nil)
	})

	t.Run("duration", func(t *testing.T) {
		var d time.Duration
		fv := &flagValue{value: &d}

		err := fv.Set("5m30s")
		assert.NilError(t, err)
		assert.Equal(t, d, 5*time.Minute+30*time.Second)
	})

	t.Run("duration invalid", func(t *testing.T) {
		var d time.Duration
		fv := &flagValue{value: &d}

		err := fv.Set("not-a-duration")
		assert.Assert(t, err != nil)
	})

	t.Run("url", func(t *testing.T) {
		var u url.URL
		fv := &flagValue{value: &u}

		err := fv.Set("https://example.com:8080/path?query=1")
		assert.NilError(t, err)
		assert.Equal(t, u.Scheme, "https")
		assert.Equal(t, u.Host, "example.com:8080")
		assert.Equal(t, u.Path, "/path")
		assert.Equal(t, u.RawQuery, "query=1")
	})

	t.Run("TextUnmarshaler", func(t *testing.T) {
		var tu testTextUnmarshaler
		fv := &flagValue{value: &tu}

		err := fv.Set("test-value")
		assert.NilError(t, err)
		assert.Equal(t, tu.Value, "unmarshaled:test-value")
	})

	t.Run("unsupported type", func(t *testing.T) {
		var c complex128
		fv := &flagValue{value: &c}

		err := fv.Set("1+2i")
		assert.ErrorContains(t, err, "unsupported type")
	})
}

// TestFlagValue_String tests the String method for all supported types.
func TestFlagValue_String(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		fv := &flagValue{value: nil}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("sensitive value", func(t *testing.T) {
		s := "secret"
		fv := &flagValue{value: &s, sensitive: true}
		assert.Equal(t, fv.String(), "********")
	})

	t.Run("string", func(t *testing.T) {
		s := "hello"
		fv := &flagValue{value: &s}
		assert.Equal(t, fv.String(), "hello")
	})

	t.Run("empty string slice", func(t *testing.T) {
		var ss []string
		fv := &flagValue{value: &ss, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("string slice", func(t *testing.T) {
		ss := []string{"a", "b", "c"}
		fv := &flagValue{value: &ss, separator: ","}
		assert.Equal(t, fv.String(), "[a, b, c]")
	})

	t.Run("string slice with space separator", func(t *testing.T) {
		ss := []string{"a", "b", "c"}
		fv := &flagValue{value: &ss, separator: " "}
		assert.Equal(t, fv.String(), "[a b c]")
	})

	t.Run("empty int slice", func(t *testing.T) {
		var is []int
		fv := &flagValue{value: &is, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("int slice", func(t *testing.T) {
		is := []int{1, 2, 3}
		fv := &flagValue{value: &is, separator: ","}
		assert.Equal(t, fv.String(), "[1, 2, 3]")
	})

	t.Run("empty int32 slice", func(t *testing.T) {
		var is []int32
		fv := &flagValue{value: &is, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("int32 slice", func(t *testing.T) {
		is := []int32{1, 2, 3}
		fv := &flagValue{value: &is, separator: ","}
		assert.Equal(t, fv.String(), "[1, 2, 3]")
	})

	t.Run("empty int64 slice", func(t *testing.T) {
		var is []int64
		fv := &flagValue{value: &is, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("int64 slice", func(t *testing.T) {
		is := []int64{1, 2, 9223372036854775807}
		fv := &flagValue{value: &is, separator: ","}
		assert.Equal(t, fv.String(), "[1, 2, 9223372036854775807]")
	})

	t.Run("empty uint64 slice", func(t *testing.T) {
		var us []uint64
		fv := &flagValue{value: &us, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("uint64 slice", func(t *testing.T) {
		us := []uint64{1, 2, 18446744073709551615}
		fv := &flagValue{value: &us, separator: ","}
		assert.Equal(t, fv.String(), "[1, 2, 18446744073709551615]")
	})

	t.Run("empty float32 slice", func(t *testing.T) {
		var fs []float32
		fv := &flagValue{value: &fs, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("float32 slice", func(t *testing.T) {
		fs := []float32{1.1, 2.2, 3.3}
		fv := &flagValue{value: &fs, separator: ","}
		assert.Equal(t, fv.String(), "[1.1, 2.2, 3.3]")
	})

	t.Run("empty float64 slice", func(t *testing.T) {
		var fs []float64
		fv := &flagValue{value: &fs, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("float64 slice", func(t *testing.T) {
		fs := []float64{1.1, 2.2, 3.14159265359}
		fv := &flagValue{value: &fs, separator: ","}
		assert.Equal(t, fv.String(), "[1.1, 2.2, 3.14159265359]")
	})

	t.Run("empty bool slice", func(t *testing.T) {
		var bs []bool
		fv := &flagValue{value: &bs, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("bool slice", func(t *testing.T) {
		bs := []bool{true, false, true}
		fv := &flagValue{value: &bs, separator: ","}
		assert.Equal(t, fv.String(), "[true, false, true]")
	})

	t.Run("empty duration slice", func(t *testing.T) {
		var ds []time.Duration
		fv := &flagValue{value: &ds, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("duration slice", func(t *testing.T) {
		ds := []time.Duration{time.Second, 2 * time.Minute, 3 * time.Hour}
		fv := &flagValue{value: &ds, separator: ","}
		assert.Equal(t, fv.String(), "[1s, 2m0s, 3h0m0s]")
	})

	t.Run("empty map", func(t *testing.T) {
		m := map[string]string{}
		fv := &flagValue{value: &m, separator: ","}
		assert.Equal(t, fv.String(), "")
	})

	t.Run("map with single entry", func(t *testing.T) {
		m := map[string]string{"key": "val"}
		fv := &flagValue{value: &m, separator: ","}
		assert.Equal(t, fv.String(), "[key=val]")
	})

	t.Run("int", func(t *testing.T) {
		i := 42
		fv := &flagValue{value: &i}
		assert.Equal(t, fv.String(), "42")
	})

	t.Run("int32", func(t *testing.T) {
		i := int32(42)
		fv := &flagValue{value: &i}
		assert.Equal(t, fv.String(), "42")
	})

	t.Run("int64", func(t *testing.T) {
		i := int64(9223372036854775807)
		fv := &flagValue{value: &i}
		assert.Equal(t, fv.String(), "9223372036854775807")
	})

	t.Run("uint64", func(t *testing.T) {
		u := uint64(18446744073709551615)
		fv := &flagValue{value: &u}
		assert.Equal(t, fv.String(), "18446744073709551615")
	})

	t.Run("float32", func(t *testing.T) {
		f := float32(3.14)
		fv := &flagValue{value: &f}
		assert.Equal(t, fv.String(), "3.14")
	})

	t.Run("float64", func(t *testing.T) {
		f := 3.14159265359
		fv := &flagValue{value: &f}
		assert.Equal(t, fv.String(), "3.14159265359")
	})

	t.Run("bool true", func(t *testing.T) {
		b := true
		fv := &flagValue{value: &b}
		assert.Equal(t, fv.String(), "true")
	})

	t.Run("bool false", func(t *testing.T) {
		b := false
		fv := &flagValue{value: &b}
		assert.Equal(t, fv.String(), "false")
	})

	t.Run("duration", func(t *testing.T) {
		d := 5*time.Minute + 30*time.Second
		fv := &flagValue{value: &d}
		assert.Equal(t, fv.String(), "5m30s")
	})

	t.Run("url", func(t *testing.T) {
		u := url.URL{Scheme: "https", Host: "example.com", Path: "/path"}
		fv := &flagValue{value: &u}
		assert.Equal(t, fv.String(), "https://example.com/path")
	})

	t.Run("Stringer type", func(t *testing.T) {
		tu := testTextUnmarshaler{Value: "custom-value"}
		fv := &flagValue{value: &tu}
		assert.Equal(t, fv.String(), "custom-value")
	})

	t.Run("fallback for unknown type", func(t *testing.T) {
		type custom struct{ X int }
		c := custom{X: 42}
		fv := &flagValue{value: &c}
		assert.Equal(t, fv.String(), "&{42}")
	})
}

// TestFlagValue_IsBoolFlag tests the IsBoolFlag method.
func TestFlagValue_IsBoolFlag(t *testing.T) {
	t.Run("bool returns true", func(t *testing.T) {
		var b bool
		fv := &flagValue{value: &b}
		assert.Assert(t, fv.IsBoolFlag())
	})

	t.Run("non-bool returns false", func(t *testing.T) {
		var s string
		fv := &flagValue{value: &s}
		assert.Assert(t, !fv.IsBoolFlag())
	})
}

// TestBind tests the Bind function.
func TestBind(t *testing.T) {
	t.Run("adds flags to command", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name" description:"The name"`
			Port int    `flag:"port" description:"The port"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test"}

		Bind(cmd, c)

		assert.Assert(t, cmd.Flags().Lookup("name") != nil)
		assert.Assert(t, cmd.Flags().Lookup("port") != nil)
	})

	t.Run("skips fields without flag tag", func(t *testing.T) {
		type cfg struct {
			Name    string `flag:"name"`
			Skipped string
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test"}

		Bind(cmd, c)

		assert.Assert(t, cmd.Flags().Lookup("name") != nil)
		assert.Assert(t, cmd.Flags().Lookup("Skipped") == nil)
	})

	t.Run("preserves existing values", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name" default:"default-name"`
			Port int    `flag:"port" default:"8080"`
		}
		c := New[cfg]()
		cmd := &cobra.Command{Use: "test"}

		Bind(cmd, c)

		assert.Equal(t, c.Name, "default-name")
		assert.Equal(t, c.Port, 8080)
	})

	t.Run("parses flags from args", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name"`
			Port int    `flag:"port"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		cmd.SetArgs([]string{"--name=custom", "--port=9000"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Name, "custom")
		assert.Equal(t, c.Port, 9000)
	})

	t.Run("bool flag with no-opt-default", func(t *testing.T) {
		type cfg struct {
			Verbose bool `flag:"verbose"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		cmd.SetArgs([]string{"--verbose"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Verbose, true)
	})

	t.Run("validates required flags", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name" required:"true"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "flag --name is required")
	})

	t.Run("required flag satisfied", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name" required:"true"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		cmd.SetArgs([]string{"--name=value"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Name, "value")
	})

	t.Run("custom separator", func(t *testing.T) {
		type cfg struct {
			Tags []string `flag:"tags" separator:";"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		cmd.SetArgs([]string{"--tags=a;b;c"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.DeepEqual(t, c.Tags, []string{"a", "b", "c"})
	})

	t.Run("panics on non-pointer", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name"`
		}
		cmd := &cobra.Command{Use: "test"}

		assert.Assert(t, panics(func() { Bind(cmd, cfg{}) }))
	})

	t.Run("panics on non-struct pointer", func(t *testing.T) {
		s := "not a struct"
		cmd := &cobra.Command{Use: "test"}

		assert.Assert(t, panics(func() { Bind(cmd, &s) }))
	})
}

// TestBindPersistent tests the BindPersistent function.
func TestBindPersistent(t *testing.T) {
	t.Run("adds flags to persistent flags", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name" description:"The name"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test"}

		BindPersistent(cmd, c)

		assert.Assert(t, cmd.PersistentFlags().Lookup("name") != nil)
		assert.Assert(t, cmd.Flags().Lookup("name") == nil)
	})

	t.Run("persistent PreRunE validates required", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name" required:"true"`
		}
		c := &cfg{}
		parent := &cobra.Command{Use: "parent"}
		child := &cobra.Command{Use: "child", Run: func(cmd *cobra.Command, args []string) {}}
		parent.AddCommand(child)

		BindPersistent(parent, c)
		parent.SetArgs([]string{"child"})

		err := parent.Execute()
		assert.ErrorContains(t, err, "flag --name is required")
	})
}

// TestEnvironmentVariableFallback tests environment variable fallback.
func TestEnvironmentVariableFallback(t *testing.T) {
	t.Run("uses env var when flag not provided", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"my-name"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		t.Setenv("SKIPPER_MY_NAME", "from-env")
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Name, "from-env")
	})

	t.Run("flag takes precedence over env var", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"my-name"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		t.Setenv("SKIPPER_MY_NAME", "from-env")
		cmd.SetArgs([]string{"--my-name=from-flag"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Name, "from-flag")
	})

	t.Run("env var satisfies required flag", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"my-name" required:"true"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		t.Setenv("SKIPPER_MY_NAME", "from-env")
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Name, "from-env")
	})

	t.Run("env var parsing error", func(t *testing.T) {
		type cfg struct {
			Port int `flag:"port"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		t.Setenv("SKIPPER_PORT", "not-a-number")
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		assert.ErrorContains(t, err, "error parsing environment variable SKIPPER_PORT")
	})

	t.Run("derives env var name correctly", func(t *testing.T) {
		type cfg struct {
			ComplexFlagName string `flag:"complex-flag-name"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		t.Setenv("SKIPPER_COMPLEX_FLAG_NAME", "test-value")
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.ComplexFlagName, "test-value")
	})
}

// TestSensitiveFlags tests sensitive flag handling.
func TestSensitiveFlags(t *testing.T) {
	t.Run("sensitive flag value is masked in String()", func(t *testing.T) {
		type cfg struct {
			Secret string `flag:"secret" sensitive:"true"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test"}

		Bind(cmd, c)

		flag := cmd.Flags().Lookup("secret")
		assert.Assert(t, flag != nil)

		// Set a value
		err := flag.Value.Set("super-secret")
		assert.NilError(t, err)

		// The value should be masked when calling String()
		assert.Equal(t, flag.Value.String(), "********")

		// But the actual struct field should have the real value
		assert.Equal(t, c.Secret, "super-secret")
	})
}

// TestTextUnmarshalerIntegration tests TextUnmarshaler support in Bind.
func TestTextUnmarshalerIntegration(t *testing.T) {
	t.Run("parses TextUnmarshaler from flag", func(t *testing.T) {
		type cfg struct {
			Custom testTextUnmarshaler `flag:"custom"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		cmd.SetArgs([]string{"--custom=hello"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Custom.Value, "unmarshaled:hello")
	})

	t.Run("preserves TextUnmarshaler default from New", func(t *testing.T) {
		type cfg struct {
			Custom testTextUnmarshaler `flag:"custom" default:"default-value"`
		}
		c := New[cfg]()
		cmd := &cobra.Command{Use: "test"}

		Bind(cmd, c)

		assert.Equal(t, c.Custom.Value, "unmarshaled:default-value")
	})

	t.Run("parses TextUnmarshaler from env var", func(t *testing.T) {
		type cfg struct {
			Custom testTextUnmarshaler `flag:"custom"`
		}
		c := &cfg{}
		cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

		Bind(cmd, c)
		t.Setenv("SKIPPER_CUSTOM", "from-env")
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Equal(t, c.Custom.Value, "unmarshaled:from-env")
	})
}

// TestPreRunEChaining tests that existing PreRunE hooks are preserved.
func TestPreRunEChaining(t *testing.T) {
	t.Run("chains with existing PreRunE", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name"`
		}
		c := &cfg{}

		existingPreRunCalled := false
		cmd := &cobra.Command{
			Use: "test",
			PreRunE: func(cmd *cobra.Command, args []string) error {
				existingPreRunCalled = true
				return nil
			},
			Run: func(cmd *cobra.Command, args []string) {},
		}

		Bind(cmd, c)
		cmd.SetArgs([]string{"--name=test"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Assert(t, existingPreRunCalled)
	})

	t.Run("chains with existing PersistentPreRunE", func(t *testing.T) {
		type cfg struct {
			Name string `flag:"name"`
		}
		c := &cfg{}

		existingPreRunCalled := false
		cmd := &cobra.Command{
			Use: "test",
			PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
				existingPreRunCalled = true
				return nil
			},
			Run: func(cmd *cobra.Command, args []string) {},
		}

		BindPersistent(cmd, c)
		cmd.SetArgs([]string{"--name=test"})

		err := cmd.Execute()
		assert.NilError(t, err)
		assert.Assert(t, existingPreRunCalled)
	})
}

// TestFlagDescriptionContainsEnvVar tests that flag descriptions include env var name.
func TestFlagDescriptionContainsEnvVar(t *testing.T) {
	type cfg struct {
		Name string `flag:"my-flag" description:"The name value"`
	}
	c := &cfg{}
	cmd := &cobra.Command{Use: "test"}

	Bind(cmd, c)

	flag := cmd.Flags().Lookup("my-flag")
	assert.Assert(t, flag != nil)
	assert.Assert(t, strings.Contains(flag.Usage, "SKIPPER_MY_FLAG"))
	assert.Assert(t, strings.Contains(flag.Usage, "The name value"))
}

// TestEmptyValues tests handling of empty values.
func TestEmptyValues(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		var s string
		fv := &flagValue{value: &s}

		err := fv.Set("")
		assert.NilError(t, err)
		assert.Equal(t, s, "")
	})

	t.Run("empty slice", func(t *testing.T) {
		var ss []string
		fv := &flagValue{value: &ss, separator: ","}

		err := fv.Set("")
		assert.NilError(t, err)
		assert.DeepEqual(t, ss, []string{""})
	})
}

// TestMultipleFlags tests binding multiple flags.
func TestMultipleFlags(t *testing.T) {
	type cfg struct {
		Host     string            `flag:"host" default:"localhost"`
		Port     int               `flag:"port" default:"8080"`
		Timeout  time.Duration     `flag:"timeout" default:"30s"`
		Verbose  bool              `flag:"verbose"`
		Tags     []string          `flag:"tags" separator:","`
		Metadata map[string]string `flag:"metadata" separator:","`
	}
	c := &cfg{}
	cmd := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, args []string) {}}

	Bind(cmd, c)
	cmd.SetArgs([]string{
		"--host=example.com",
		"--port=9000",
		"--timeout=1m",
		"--verbose",
		"--tags=a,b,c",
		"--metadata=key1=val1,key2=val2",
	})

	err := cmd.Execute()
	assert.NilError(t, err)
	assert.Equal(t, c.Host, "example.com")
	assert.Equal(t, c.Port, 9000)
	assert.Equal(t, c.Timeout, time.Minute)
	assert.Equal(t, c.Verbose, true)
	assert.DeepEqual(t, c.Tags, []string{"a", "b", "c"})
	assert.DeepEqual(t, c.Metadata, map[string]string{"key1": "val1", "key2": "val2"})
}

// TestNew tests the generic New function.
func TestNew(t *testing.T) {
	t.Run("returns pointer to new struct with defaults", func(t *testing.T) {
		type cfg struct {
			Host    string        `default:"localhost"`
			Port    int           `default:"8080"`
			Timeout time.Duration `default:"30s"`
		}

		c := New[cfg]()

		assert.Equal(t, c.Host, "localhost")
		assert.Equal(t, c.Port, 8080)
		assert.Equal(t, c.Timeout, 30*time.Second)
	})

	t.Run("returns independent instances", func(t *testing.T) {
		type cfg struct {
			Name string `default:"default"`
		}

		c1 := New[cfg]()
		c2 := New[cfg]()

		c1.Name = "modified"

		assert.Equal(t, c1.Name, "modified")
		assert.Equal(t, c2.Name, "default")
	})
}

// TestDefault tests the Default function.
func TestDefault(t *testing.T) {
	t.Run("applies default values from struct tags", func(t *testing.T) {
		type cfg struct {
			Host    string        `default:"localhost"`
			Port    int           `default:"8080"`
			Timeout time.Duration `default:"30s"`
			Verbose bool          `default:"true"`
			Rate    float64       `default:"1.5"`
		}
		c := &cfg{}

		Default(c)

		assert.Equal(t, c.Host, "localhost")
		assert.Equal(t, c.Port, 8080)
		assert.Equal(t, c.Timeout, 30*time.Second)
		assert.Equal(t, c.Verbose, true)
		assert.Equal(t, c.Rate, 1.5)
	})

	t.Run("skips fields without default tag", func(t *testing.T) {
		type cfg struct {
			Name      string `default:"default-name"`
			NoDefault string
		}
		c := &cfg{}

		Default(c)

		assert.Equal(t, c.Name, "default-name")
		assert.Equal(t, c.NoDefault, "")
	})

	t.Run("uses custom separator", func(t *testing.T) {
		type cfg struct {
			Tags []string `default:"a;b;c" separator:";"`
		}
		c := &cfg{}

		Default(c)

		assert.DeepEqual(t, c.Tags, []string{"a", "b", "c"})
	})

	t.Run("uses default separator for slices", func(t *testing.T) {
		type cfg struct {
			Tags []string `default:"a,b,c"`
		}
		c := &cfg{}

		Default(c)

		assert.DeepEqual(t, c.Tags, []string{"a", "b", "c"})
	})

	t.Run("applies TextUnmarshaler defaults", func(t *testing.T) {
		type cfg struct {
			Custom testTextUnmarshaler `default:"test-value"`
		}
		c := &cfg{}

		Default(c)

		assert.Equal(t, c.Custom.Value, "unmarshaled:test-value")
	})

	t.Run("panics on non-pointer", func(t *testing.T) {
		type cfg struct {
			Name string `default:"test"`
		}

		assert.Assert(t, panics(func() { Default(cfg{}) }))
	})

	t.Run("panics on non-struct pointer", func(t *testing.T) {
		s := "not a struct"

		assert.Assert(t, panics(func() { Default(&s) }))
	})

	t.Run("panics on invalid default value", func(t *testing.T) {
		type cfg struct {
			Port int `default:"not-a-number"`
		}
		c := &cfg{}

		assert.Assert(t, panics(func() { Default(c) }))
	})

	t.Run("works with all supported types", func(t *testing.T) {
		type cfg struct {
			String   string        `default:"hello"`
			Int      int           `default:"42"`
			Int32    int32         `default:"32"`
			Int64    int64         `default:"64"`
			Uint64   uint64        `default:"100"`
			Float32  float32       `default:"3.14"`
			Float64  float64       `default:"2.718"`
			Bool     bool          `default:"true"`
			Duration time.Duration `default:"1h30m"`
			URL      url.URL       `default:"https://example.com"`
		}
		c := &cfg{}

		Default(c)

		assert.Equal(t, c.String, "hello")
		assert.Equal(t, c.Int, 42)
		assert.Equal(t, c.Int32, int32(32))
		assert.Equal(t, c.Int64, int64(64))
		assert.Equal(t, c.Uint64, uint64(100))
		assert.Equal(t, c.Float32, float32(3.14))
		assert.Equal(t, c.Float64, 2.718)
		assert.Equal(t, c.Bool, true)
		assert.Equal(t, c.Duration, time.Hour+30*time.Minute)
		assert.Equal(t, c.URL.String(), "https://example.com")
	})
}

// Helper functions

func panics(f func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	f()
	return false
}
