package telemetry

type Component int

const (
	ComponentController Component = iota
	ComponentRouter
)

func (c Component) String() string {
	switch c {
	case ComponentController:
		return "controller"
	case ComponentRouter:
		return "router"
	default:
		return "unknown"
	}
}
