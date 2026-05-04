package main

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestFormatLatency(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ms   float64
		want string
	}{
		// sub-millisecond, 2 decimal places
		{0.45, "0.45ms"},
		{0.01, "0.01ms"},
		{0.999, "1.00ms"},

		// 1-10ms, 1 decimal place
		{1, "1.0ms"},
		{2.34, "2.3ms"},
		{9.99, "10.0ms"},

		// 10-1000ms, integers
		{10, "10ms"},
		{45.6, "46ms"},
		{999, "999ms"},

		// >= 1s in seconds
		{1000, "1.0s"},
		{1500, "1.5s"},
		{12345, "12.3s"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, formatLatency(tc.ms), tc.want)
		})
	}
}

func TestGetPercentile(t *testing.T) {
	t.Parallel()

	t.Run("returns 0 for empty slice", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, getPercentile(nil, 0.5), 0.0)
	})

	t.Run("single element", func(t *testing.T) {
		t.Parallel()
		s := []float64{42}
		assert.Equal(t, getPercentile(s, 0), 42.0)
		assert.Equal(t, getPercentile(s, 0.5), 42.0)
		assert.Equal(t, getPercentile(s, 1), 42.0)
	})

	t.Run("exact boundaries", func(t *testing.T) {
		t.Parallel()
		s := []float64{1, 2, 3, 4, 5}
		assert.Equal(t, getPercentile(s, 0), 1.0)
		assert.Equal(t, getPercentile(s, 1), 5.0)
	})

	t.Run("median odd-length", func(t *testing.T) {
		t.Parallel()
		s := []float64{1, 2, 3, 4, 5}
		assert.Equal(t, getPercentile(s, 0.5), 3.0)
	})

	t.Run("median even-length interpolates", func(t *testing.T) {
		t.Parallel()
		s := []float64{1, 2, 3, 4}
		assert.Equal(t, getPercentile(s, 0.5), 2.5)
	})

	t.Run("p90 fractional index", func(t *testing.T) {
		t.Parallel()
		s := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
		// index = 9 * 0.9 = 8.1 -> interpolate between sorted[8]=90 and sorted[9]=100
		got := getPercentile(s, 0.9)
		assert.Assert(t, got > 90.9 && got < 91.1, "expected ~91, got %v", got)
	})

	t.Run("p99 on large array", func(t *testing.T) {
		t.Parallel()
		s := make([]float64, 1000)
		for i := range s {
			s[i] = float64(i + 1)
		}
		got := getPercentile(s, 0.99)
		assert.Assert(t, got > 990 && got < 991, "expected ~990.01, got %v", got)
	})
}
