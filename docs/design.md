# Fusion

## Scaling

- Need to scale to 0 when a tenant hasn't received any requests for a while
- Need to scale to 1 when a tenant receives a request and they don't have any assigned pods
- Need to scale to up/down when cpu or memory utilization is high/low
  - The cpu and memory utilization of a tenant is provided via the `x-fusion-cpu` and `x-fusion-memory` headers.
  - N is provided via the `x-fusion-replicas` header.
  - The headers are stored as labels on the tenant's pod. The controller
    uses the labels on the pod with the latest `fusion/assigned-at`
    timestamp.

## Components

- Router
- Controller w/ hashring or leader election

## Notes

- need to re-assign all functions when deployment is updated
