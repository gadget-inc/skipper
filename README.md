# Skipper

Skipper is a Kubernetes controller that turns Kubernetes deployments into a pool of functions that can be assigned to tenants. It handles pod assignment, request routing, autoscaling, and idle reclamation automatically.

**Full documentation lives at [gadget-inc.github.io/skipper](https://gadget-inc.github.io/skipper/).**

- [Introduction](https://gadget-inc.github.io/skipper/) — what Skipper does and how it works
- [Deploying Functions](https://gadget-inc.github.io/skipper/guides/deploying-functions/) — label your deployments and configure function pools
- [Routing and Proxying](https://gadget-inc.github.io/skipper/guides/routing/) — the `X-Skipper-Function` header, request resolution, and WebSocket support
- [Scaling](https://gadget-inc.github.io/skipper/guides/scaling/) — autoscaling behavior, metrics, and tuning
- [Architecture](https://gadget-inc.github.io/skipper/architecture/overview/) — how the Controller and Router fit together
- [Reference](https://gadget-inc.github.io/skipper/reference/configuration/) — configuration flags and the gRPC API

If you're looking to contribute, see [CONTRIBUTING.md](CONTRIBUTING.md).
