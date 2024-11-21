package flag

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrap(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		flag := Flag[string]{}

		err := flag.Set("foo")
		assert.NoError(t, err)
		assert.Equal(t, "foo", flag.Value)
		assert.Equal(t, "foo", flag.String())
	})

	t.Run("strings", func(t *testing.T) {
		type test struct {
			name           string
			input          string
			separator      string
			expected       []string
			expectedString string
		}

		tests := []test{
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

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				flag := Flag[[]string]{Separator: test.separator}

				err := flag.Set(test.input)
				assert.NoError(t, err)
				assert.Equal(t, test.expected, flag.Value)
				assert.Equal(t, test.expectedString, flag.String())
			})
		}
	})
}
