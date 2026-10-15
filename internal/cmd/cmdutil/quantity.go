package cmdutil

import (
	"fmt"
	"regexp"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// cpuQuantity permits an integer count of vCPUs ("2"), a millicore value
// ("500m"), or a fractional vCPU count ("1.5", ".5"). It is the cluster and
// database CRD pattern, so the CLI's format check matches the server's
// without pulling in the Kubernetes quantity parser.
var cpuQuantity = regexp.MustCompile(`^(\d+m?|\d*\.\d+)$`)

// unitQuantity requires an explicit unit suffix. The server rejects bare
// numbers for memory and storage as ambiguous (8 bytes vs 8 GiB).
var unitQuantity = regexp.MustCompile(`^\d+(\.\d+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)$`)

// IsCPUQuantity reports whether s is a valid CPU quantity ("2", "1.5", "500m").
func IsCPUQuantity(s string) bool { return cpuQuantity.MatchString(s) }

// IsUnitQuantity reports whether s is a memory or storage quantity with a
// unit suffix ("8Gi", "512Mi", "50G").
func IsUnitQuantity(s string) bool { return unitQuantity.MatchString(s) }

// QuantityFlag is one CPU, memory or storage flag of a create or update
// command, as ValidateQuantities checks it.
type QuantityFlag struct {
	// Name is the flag as typed, e.g. "--cpu-limit".
	Name  string
	Value string
	// CPU marks a CPU count ("2", "500m"); otherwise the value is memory
	// or storage and needs a unit suffix.
	CPU      bool
	Required bool
	// Example is a valid value shown in hints, e.g. "500m".
	Example string
}

// ValidateQuantities checks resource flags before a request, so a user sees
// flag-shaped errors instead of the server's field paths: first that every
// required flag is set, then that no value is longer than maxLength (0: no
// cap), then each set value's format. The server stays authoritative for
// floors, ceilings and the plan's pool.
func ValidateQuantities(maxLength int, flags ...QuantityFlag) error {
	for _, q := range flags {
		if q.Required && q.Value == "" {
			return cli.MisuseError(q.Name + " is required").WithHint(q.example() + q.unitNote())
		}
	}
	for _, q := range flags {
		if maxLength > 0 && len(q.Value) > maxLength {
			return cli.MisuseError(fmt.Sprintf("%s is longer than %d characters: %s", q.Name, maxLength, q.Value))
		}
	}
	for _, q := range flags {
		switch {
		case q.Value == "":
		case q.CPU && !IsCPUQuantity(q.Value):
			return cli.MisuseError(q.Name + " has an invalid format: " + q.Value).
				WithHint("use a number of vCPUs (e.g. 2, 0.5) or millicores (e.g. 500m), without a unit")
		case !q.CPU && !IsUnitQuantity(q.Value):
			return cli.MisuseError(q.Name + " must include a unit suffix; got: " + q.Value).
				WithHint(q.example() + " (plain numbers are ambiguous)")
		}
	}
	return nil
}

func (q QuantityFlag) example() string {
	return "example: " + q.Name + " " + q.Example
}

// unitNote says what form the value takes, for a required flag's hint.
func (q QuantityFlag) unitNote() string {
	if q.CPU {
		return " (vCPUs, or millicores like 500m)"
	}
	return " (a unit suffix such as Gi or Mi is required)"
}
