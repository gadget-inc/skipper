package controller

import (
	"testing"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/fixture"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/go-json-experiment/json"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
)

func testConfig() *Config {
	cfg := config.New[Config]()
	cfg.Namespace = fixture.ControllerNamespace
	cfg.PodIP = fixture.ControllerIP
	cfg.PasetoPrivateKey = PasetoPrivateKey{V2AsymmetricSecretKey: fixture.ControllerPasetoSecretKey}
	cfg.FunctionNamespaces = []string{fixture.FunctionNamespace}
	return cfg
}

func ensurePodIsAssignedToFunction(t *testing.T, pod v1.Pod, fn *skipper.Function) {
	fnJSON, err := json.Marshal(fn)
	assert.NilError(t, err)

	assert.Assert(t, fn.GetDeployment() == pod.Labels[key.Deployment.Label])
	assert.Assert(t, fn.GetTenant() == pod.Labels[key.Tenant.Label])
	assert.Assert(t, string(fnJSON) == pod.Annotations[key.Function.Annotation])
	assert.Assert(t, pod.Annotations[key.ReplicaSet.Annotation] != "")
	assert.Assert(t, pod.Annotations[key.AssignedAt.Annotation] != "")
	assert.Assert(t, pod.Annotations[key.ReadyAt.Annotation] != "")
}

func countReadyAndUnreadyPods(pods []v1.Pod) (ready, unready int) {
	for _, pod := range pods {
		if isPodReady(&pod) {
			ready++
		} else if isPodRunning(&pod) {
			unready++
		}
	}
	return
}
