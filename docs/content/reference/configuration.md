---
title: Configuration Reference
description: Complete reference for all controller and router configuration options.
---

Every flag has an environment variable equivalent: `--flag-name` becomes `SKIPPER_FLAG_NAME`. CLI flags take precedence over environment variables. Sensitive values (private keys) are shown as `********` in help output.

## Controller configuration

{{< flagtable controller >}}

## Router configuration

{{< flagtable router >}}

## Shared configuration

These flags are available on both the controller and the router.

{{< flagtable shared >}}

## Environment variable substitution

The pattern for deriving environment variable names from flags:

1. Uppercase the flag name
2. Replace hyphens with underscores
3. Prefix with `SKIPPER_`

For example, `--heartbeat-timeout` becomes `SKIPPER_HEARTBEAT_TIMEOUT`.

This is particularly useful in Kubernetes manifests:

```yaml
env:
  - name: SKIPPER_POD_IP
    valueFrom:
      fieldRef:
        fieldPath: status.podIP
  - name: SKIPPER_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
```
