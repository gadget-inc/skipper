# Skipper

Skipper is a Kubernetes controller that turns Kubernetes deployments into a pool of functions that can be assigned to tenants.

## Overview

Assume you have the following echo server:

```js
const server = Deno.serve({ port: 3000 }, async (request) => {
  const method = request.method;
  const path = new URL(request.url).pathname;
  const headers = Object.fromEntries(request.headers.entries());
  const body = await request.text();

  return new Response(JSON.stringify({ method, path, headers, body }), {
    headers: { "content-type": "application/json" },
  });
});

Deno.addSignalListener("SIGTERM", async () => {
  await server.shutdown();
});

await server.finished;
```

Create a Kubernetes deployment with the `skipper/deployment` label, `skipper/tenant` DoesNotExist match expression, and `skipper/port`
annotation.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echo-server
  labels:
    skipper/deployment: echo-server
spec:
  replicas: 2
  selector:
    matchLabels:
      skipper/deployment: echo-server
    matchExpressions:
      - key: skipper/tenant
        operator: DoesNotExist
  template:
    metadata:
      labels:
        skipper/deployment: echo-server
      annotations:
        skipper/port: "http"
    spec:
      containers:
        - image: echo-server:latest
          ports:
            - name: http
              containerPort: 3000
```

> [!NOTE]
> The `skipper/port` annotation isn't required, but your deployment must have at least one annotation defined, otherwise Skipper won't be
> able to atomically add annotations to your pods because the JSON Patch will fail. See
> [this Stack Overflow answer](https://stackoverflow.com/a/57480206) and
> [this section of the JSON Patch RFC](https://datatracker.ietf.org/doc/html/rfc6902#appendix-A.12) for more details.

This deployment can now be used as a pool of echo servers that are ready to be assigned to tenants. You can assign one of these echo servers
to a tenant by sending a request to Skipper's router with the `x-skipper-function` header:

```js
const routerUrl = "http://skipper-production-router.skipper-production.svc.cluster.local";

const response = await fetch(`${routerUrl}/some-path`, {
  method: "POST",
  headers: {
    "x-skipper-function": JSON.stringify({ namespace: "default", deployment: "echo-server", tenant: "123" }),
    "content-type": "application/json",
  },
  body: JSON.stringify({ hello: "world" }),
});

const body = await response.json();
console.log(body);
// {
//   method: "POST",
//   path: "/some-path",
//   headers: {
//     "content-type": "application/json",
//     "content-length": "17",
//   },
//   body: '{"hello":"world"}'
// }
```

Skipper will assign one of the echo servers to the tenant and return the response from the assigned echo server. All subsequent requests to
the same function will be sent to the same echo server. If the function doesn't receive a request within the
`SKIPPER_CONTROLLER_HEARTBEAT_TIMEOUT` (default: 90s), the pod will be terminated.
