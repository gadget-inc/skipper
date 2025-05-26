package flag

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestFlag(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		flag := Flag[string]{}

		err := flag.Set("foo")
		assert.NilError(t, err)
		assert.Assert(t, flag.Value() == "foo")
		assert.Assert(t, flag.String() == "foo")
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
				assert.NilError(t, err)
				assert.DeepEqual(t, flag.Value(), tc.expected)
				assert.Assert(t, flag.String() == tc.expectedString)
			})
		}
	})
}
