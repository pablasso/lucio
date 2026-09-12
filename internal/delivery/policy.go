package delivery

import "fmt"

// Policy names the caller's preferred transport/update strategy. Clients still
// submit a complete target image; the long-running lucio daemon decides how to
// deliver that target to the physical screen.
type Policy string

const (
	PolicyFull    Policy = "full"
	PolicyAuto    Policy = "auto"
	PolicyPartial Policy = "partial"
)

const DefaultPolicy = PolicyFull

func ParsePolicy(raw string) (Policy, error) {
	if raw == "" {
		return DefaultPolicy, nil
	}
	switch Policy(raw) {
	case PolicyFull, PolicyAuto, PolicyPartial:
		return Policy(raw), nil
	default:
		return "", fmt.Errorf("update policy must be one of %s", Choices())
	}
}

func Choices() string { return "full, auto, partial" }
