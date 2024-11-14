# Fusion

## Scaling

- Need to scale to 1 when a tenant receives a request and they don't have any assigned pods
- Need to scale to 0 when a tenant hasn't received any requests for a while
- Need to scale to 2 when cpu or memory usage is high

## Components

- Router
- Controller w/ leader election
