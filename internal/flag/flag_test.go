package flag

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestFlag(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		flag := Flag[string]{}

		err := flag.Set("foo")
		must.NoError(t, err)
		must.Eq(t, "foo", flag.Value())
		must.Eq(t, "foo", flag.String())
	})

	t.Run("strings", func(t *testing.T) {
		testCases := []struct {
			name           string
			input          string
			separator      string
			expected       []string
			expectedString string
		}{
			{
				name:           "empty",
				input:          "foo,bar",
				separator:      "",
				expected:       []string{"foo", "bar"},
				expectedString: "[foo, bar]",
			},
			{
				name:           "comma",
				input:          "foo,bar",
				separator:      ",",
				expected:       []string{"foo", "bar"},
				expectedString: "[foo, bar]",
			},
			{
				name:           "space",
				input:          "foo bar",
				separator:      " ",
				expected:       []string{"foo", "bar"},
				expectedString: "[foo bar]",
			},
			{
				name:           "mismatched",
				input:          "foo,bar",
				separator:      " ",
				expected:       []string{"foo,bar"},
				expectedString: "[foo,bar]",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				flag := Flag[[]string]{Separator: tc.separator}

				err := flag.Set(tc.input)
				must.NoError(t, err)
				must.Eq(t, tc.expected, flag.Value())
				must.Eq(t, tc.expectedString, flag.String())
			})
		}
	})
}
