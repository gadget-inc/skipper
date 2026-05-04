package resolver

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	discoverylisters "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/tools/cache"

	"google.golang.org/grpc/resolver"
)

// Builder implements resolver.Builder using Kubernetes EndpointSlice informers
// for zone-aware, request-level load balancing.
type Builder struct {
	client      kubernetes.Interface
	namespace   string
	serviceName string
	port        int
	zone        string
}

var _ resolver.Builder = (*Builder)(nil)

// New creates a new Builder. The port is appended to each endpoint address to
// form host:port pairs for gRPC. The zone parameter may be empty, in which
// case all ready endpoints are used (graceful degradation).
func New(client kubernetes.Interface, namespace, serviceName string, port int, zone string) *Builder {
	return &Builder{
		client:      client,
		namespace:   namespace,
		serviceName: serviceName,
		port:        port,
		zone:        zone,
	}
}

// Scheme returns the resolver scheme.
func (b *Builder) Scheme() string { return "k8s" }

// Build is called by gRPC when creating a new client connection. It starts
// an EndpointSlice informer in a background goroutine that watches for changes
// and updates the resolver state. Close() stops the informer.
func (b *Builder) Build(_ resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	ctx, cancel := context.WithCancel(context.Background())

	r := &k8sResolver{
		client:      b.client,
		namespace:   b.namespace,
		serviceName: b.serviceName,
		port:        b.port,
		zone:        b.zone,
		cc:          cc,
		cancel:      cancel,
	}

	go r.run(ctx)

	return r, nil
}

// k8sResolver implements resolver.Resolver. Each instance owns its informer
// goroutine and per-connection state.
type k8sResolver struct {
	client      kubernetes.Interface
	namespace   string
	serviceName string
	port        int
	zone        string
	cc          resolver.ClientConn

	mu     sync.Mutex
	lister discoverylisters.EndpointSliceLister
	cancel context.CancelFunc
}

var _ resolver.Resolver = (*k8sResolver)(nil)

// ResolveNow is a no-op — the informer provides watch-based updates.
func (r *k8sResolver) ResolveNow(resolver.ResolveNowOptions) {}

// Close cancels the informer context.
func (r *k8sResolver) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *k8sResolver) run(ctx context.Context) {
	factory := informers.NewSharedInformerFactoryWithOptions(
		r.client,
		5*time.Minute,
		informers.WithNamespace(r.namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = fmt.Sprintf("%s=%s", discoveryv1.LabelServiceName, r.serviceName)
		}),
	)

	epInformer := factory.Discovery().V1().EndpointSlices()

	r.mu.Lock()
	r.lister = epInformer.Lister()
	r.mu.Unlock()

	_, err := epInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ any) { r.resolve(ctx) },
		UpdateFunc: func(_, _ any) { r.resolve(ctx) },
		DeleteFunc: func(_ any) { r.resolve(ctx) },
	})
	if err != nil {
		log.Error(ctx, "failed to add EndpointSlice event handler", key.Error.Slog(err))
		return
	}

	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), epInformer.Informer().HasSynced) {
		return
	}

	// Explicit resolution after cache sync ensures state is pushed even if
	// all informer events fired before the lister was set.
	r.resolve(ctx)

	<-ctx.Done()
}

func (r *k8sResolver) resolve(ctx context.Context) {
	r.mu.Lock()
	lister := r.lister
	r.mu.Unlock()

	if lister == nil {
		return
	}

	slices, err := lister.EndpointSlices(r.namespace).List(labels.Everything())
	if err != nil {
		log.Error(ctx, "failed to list EndpointSlices", key.Error.Slog(err))
		return
	}

	portStr := strconv.Itoa(r.port)
	var sameZone, allReady []resolver.Address

	for _, slice := range slices {
		for _, ep := range slice.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
				continue
			}
			for _, addr := range ep.Addresses {
				a := resolver.Address{Addr: net.JoinHostPort(addr, portStr)}
				allReady = append(allReady, a)
				if r.zone != "" && ep.Zone != nil && *ep.Zone == r.zone {
					sameZone = append(sameZone, a)
				}
			}
		}
	}

	addrs := allReady
	zoneMatch := "all_zones"
	if len(sameZone) > 0 {
		addrs = sameZone
		zoneMatch = "same_zone"
	}

	endpoints := make([]resolver.Endpoint, len(addrs))
	for i, a := range addrs {
		endpoints[i] = resolver.Endpoint{Addresses: []resolver.Address{a}}
	}

	if err := r.cc.UpdateState(resolver.State{Endpoints: endpoints}); err != nil {
		log.Warn(ctx, "failed to update resolver state", key.Error.Slog(err))
	}

	log.Debug(ctx, "resolved endpoints",
		key.Count.Slog(len(addrs)),
		key.ZoneMatch.Slog(zoneMatch),
		key.Zone.Slog(r.zone),
	)
}
