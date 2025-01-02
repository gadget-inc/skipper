package fixture

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/shoenig/test/must"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	RouterURL = "http://127.0.0.1:31021"
)

type Fixture struct {
	t         *testing.T
	Clientset kubernetes.Interface
}

func New(t *testing.T) *Fixture {
	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
	must.NoError(t, err)

	clientset, err := kubernetes.NewForConfig(config)
	must.NoError(t, err)

	return &Fixture{
		t:         t,
		Clientset: clientset,
	}
}

func (f *Fixture) NewRouterRequest(ctx context.Context, method, path string, body io.Reader) *http.Request {
	req, err := http.NewRequestWithContext(ctx, method, RouterURL+path, body)
	must.NoError(f.t, err)
	return req
}

func (f *Fixture) SendRouterRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req := f.NewRouterRequest(ctx, method, path, body)
	return http.DefaultClient.Do(req)
}
