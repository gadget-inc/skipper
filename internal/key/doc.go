// Package key provides typed keys for structured logging and telemetry.
//
// The key package solves the problem of maintaining consistent attribute names
// across different contexts: slog logging, OpenTelemetry tracing, HTTP headers,
// and Kubernetes labels/annotations. A single key definition automatically
// generates the appropriate name format for each context.
//
// # Key vs Identifier
//
// The package provides two types:
//
//   - [Key] is a typed key for creating structured log and telemetry attributes.
//     Use Key when you need to log or trace values.
//
//   - [Identifier] provides naming conventions (underscored, header, label,
//     annotation) without the logging machinery. Use Identifier for keys that
//     are only used for HTTP headers or Kubernetes labels/annotations.
//
// # Usage
//
// Pre-defined keys are available as package variables:
//
//	log.Info(ctx, "processing request", key.Function.Slog(fn))
//
// For telemetry context propagation (both logging and tracing):
//
//	ctx = telemetry.With(ctx, key.Function.Attr(fn))
//
// For Kubernetes labels, annotations, and HTTP headers (works with both Key and Identifier):
//
//	pod.Labels[key.Tenant.Label] = tenant
//	pod.Annotations[key.Function.Annotation] = fnJSON
//	req.Header.Set(key.Token.Header, token)
package key
