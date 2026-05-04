package docssite

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gadget-inc/skipper/internal/skipper"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// errServiceMethodUnknown is returned (wrapped) when a serviceMethod
// shortcode names a method not on the ControllerService descriptor.
// The wrap names the offending argument and lists the registered
// methods so authors can spot the typo.
var errServiceMethodUnknown = errors.New("docssite: serviceMethod shortcode references unknown method")

// controllerServiceName is the proto service name the shortcode
// resolves against. Hard-coded because Skipper's API surface is a
// single service; if a second service is ever defined, this becomes
// a registry like messageTableRegistry.
const controllerServiceName = "ControllerService"

// serviceMethodInvocationsRe matches every `{{< serviceMethod X >}}`
// invocation in a markdown body, capturing the method name. Used by
// the api.md coverage test to assert the doc references every
// descriptor method.
var serviceMethodInvocationsRe = regexp.MustCompile(`\{\{<\s*serviceMethod\s+([A-Z][A-Za-z0-9]*)\s*>\}\}`)

// renderServiceMethod returns a fenced ```protobuf block whose body
// is `rpc <Name>(<Request>) returns (<Response>)`, derived from the
// live [ControllerService] descriptor. An unknown method name
// produces a wrapped errServiceMethodUnknown listing every
// descriptor method.
func renderServiceMethod(name string) (string, error) {
	svc := skipper.File_controller_proto.Services().ByName(controllerServiceName)
	if svc == nil {
		return "", fmt.Errorf("docssite: %s not found on file descriptor", controllerServiceName)
	}
	method := svc.Methods().ByName(protoreflect.Name(name))
	if method == nil {
		return "", fmt.Errorf("%w: %s (registered: %s)",
			errServiceMethodUnknown, name, listServiceMethodNames(svc))
	}

	var b strings.Builder
	b.WriteString("```protobuf\n")
	fmt.Fprintf(&b, "rpc %s(%s) returns (%s)\n",
		method.Name(),
		method.Input().Name(),
		method.Output().Name(),
	)
	b.WriteString("```\n")
	return b.String(), nil
}

// listServiceMethodNames returns "GetInstance, Heartbeat, ..." for
// the error message.
func listServiceMethodNames(svc protoreflect.ServiceDescriptor) string {
	methods := svc.Methods()
	names := make([]string, 0, methods.Len())
	for i := range methods.Len() {
		names = append(names, string(methods.Get(i).Name()))
	}
	return strings.Join(names, ", ")
}
