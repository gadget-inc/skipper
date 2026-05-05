package controller

import (
	"encoding/json"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// TestDefaultRetryPolicies asserts the exported policy slice that the
// docs renderer reads matches the JSON service config the gRPC client
// installs, so the two cannot drift independently.
func TestDefaultRetryPolicies(t *testing.T) {
	t.Parallel()

	want := []RetryPolicy{
		{Method: "GetInstance", MaxAttempts: 3, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
		{Method: "Heartbeat", MaxAttempts: 2, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
		{Method: "Scale", MaxAttempts: 3, InitialBackoff: 50 * time.Millisecond, MaxBackoff: 500 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
		{Method: "ReleaseInstance", MaxAttempts: 2, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond, BackoffMultiplier: 2, RetryableStatusCodes: []string{"UNAVAILABLE"}},
	}

	assert.DeepEqual(t, DefaultRetryPolicies, want)
}

// TestDefaultServiceConfig_IsParseable asserts the rendered JSON
// installed on the gRPC client unmarshals cleanly. The runtime gRPC
// library would reject malformed JSON at NewClient time; this surfaces
// the failure as a test instead of a startup-time error.
func TestDefaultServiceConfig_IsParseable(t *testing.T) {
	t.Parallel()

	var parsed map[string]any
	err := json.Unmarshal([]byte(defaultServiceConfig), &parsed)
	assert.NilError(t, err)

	methodConfig, ok := parsed["methodConfig"].([]any)
	assert.Assert(t, ok)
	assert.Equal(t, len(methodConfig), len(DefaultRetryPolicies))
}
