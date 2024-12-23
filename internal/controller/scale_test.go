package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/gadget-inc/fusion/internal/fixture"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	fakemetricsclientset "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func init() {
	function.FlagAssignPath.Init()
	function.FlagAssignTimeout.Init()
	function.FlagNamespaces.Init()
	function.FlagPort.Init()

	FlagIP.SetValue("127.0.0.1")
	FlagNamespace.SetValue("fusion-test")

	// log.FlagLogLevel.SetValue(log.LevelTrace)
	// log.FlagLogFormat.SetValue("text")
	// log.Init()
}

func TestScaleFunction(t *testing.T) {
	fn := fixture.NewFunction()
	fixture.SetFlag(t, &function.FlagNamespaces, []string{fn.Namespace})

	controllerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "controller",
			Namespace: FlagNamespace.Value(),
			Labels: map[string]string{
				"app.kubernetes.io/name":      "fusion",
				"app.kubernetes.io/component": "controller",
			},
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
			PodIP: FlagIP.Value(),
		},
	}

	availablePod := fixture.AvailablePod(t, fn, func(rw http.ResponseWriter, req *http.Request) {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, function.FlagAssignPath.Value(), req.URL.Path)

		assignedFn, err := function.FromHeaders(req)
		require.NoError(t, err)
		require.Equal(t, fn, assignedFn)

		rw.WriteHeader(http.StatusOK)
	})

	clientset := fake.NewClientset(controllerPod, availablePod)
	metricsClientset := fakemetricsclientset.NewSimpleClientset()

	c := New(clientset, metricsClientset)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := c.startControllerInformer(ctx)
	require.NoError(t, err)

	err = c.startPodInformers(ctx)
	require.NoError(t, err)

	instances, err := c.scaleFunction(ctx, fn, 1)
	require.NoError(t, err)
	require.Len(t, instances, 1)
}
