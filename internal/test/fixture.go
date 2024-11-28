package test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/gadget-inc/fusion/internal/function"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type Fixture struct {
	t         *testing.T
	name      string
	clientset kubernetes.Interface
	Function  function.Function
}

func NewFixture(t *testing.T, name string) *Fixture {
	config, err := rest.InClusterConfig()
	if errors.Is(err, rest.ErrNotInCluster) {
		config, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	}
	assert.NoError(t, err, "failed to load kubernetes config")

	clientset, err := kubernetes.NewForConfig(config)
	assert.NoError(t, err, "failed to create kubernetes clientset")

	return &Fixture{
		t:         t,
		name:      name,
		clientset: clientset,
		Function: function.Function{
			Deployment:                 "example-deno",
			MaxReplicas:                1,
			MaxReplicasStr:             "1",
			Metadata:                   uuid.NewString(),
			MinReplicas:                0,
			MinReplicasStr:             "0",
			Namespace:                  "example-development",
			TargetCPUUtilization:       100,
			TargetCPUUtilizationStr:    "100",
			TargetMemoryUtilization:    200,
			TargetMemoryUtilizationStr: "200",
			Tenant:                     name + "-" + uuid.NewString(),
		},
	}
}
