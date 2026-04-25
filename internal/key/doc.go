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
//   - [Key] is a typed key for creating structured log and telemetry
//     attributes. Use Key when you need to log or trace values.
//
//   - [Identifier] provides naming conventions (underscored, header, label,
//     annotation) without the logging machinery. Use Identifier for keys that
//     are only used for HTTP headers or Kubernetes labels/annotations.
//
// # Declaring keys
//
// Generic, primitive, and external-type keys live in this package as
// pre-defined variables (Count, Namespace, Tenant, Request, Pod, ...). Domain
// types (Function, Heartbeat, Instance, Scale, ...) declare their own typed
// keys next to the type, via the public constructors:
//
//   - [New] -- general typed key; valueOf returns slog.Value.
//   - [NewCached] -- per-pointer memoized key, for long-lived pointers reused
//     across many calls. Cache entries shrink automatically once the source
//     pointer becomes unreachable.
//   - [NewLogValuer] -- shorthand for a key whose source type implements
//     slog.LogValuer; saves the (*T).LogValue method-value boilerplate.
//   - [NewLogAndOtel] -- shorthand for a key whose source type implements
//     both LogValue and an OtelAttrs() override that bypasses the slog walk.
//
// # Usage
//
// Logging:
//
//	log.Info(ctx, "processing request", skipper.FunctionKey.Slog(fn))
//
// Telemetry context propagation (logs + traces):
//
//	ctx = telemetry.With(ctx, skipper.FunctionKey.Attr(fn))
//
// Kubernetes labels, annotations, and HTTP headers (works with both Key and
// Identifier):
//
//	pod.Labels[key.Tenant.Label] = tenant
//	pod.Annotations[skipper.FunctionKey.Annotation] = fnJSON
//	req.Header.Set(key.Token.Header, token)
package key
