package cmdutil

import "regexp"

// cpuQuantity permits an integer count of vCPUs ("2"), a millicore value
// ("500m"), or a fractional vCPU count ("1.5"). Mirrors what the server
// accepts without pulling in the Kubernetes quantity parser.
var cpuQuantity = regexp.MustCompile(`^\d+(\.\d+)?m?$`)

// unitQuantity requires an explicit unit suffix. The server rejects bare
// numbers for memory and storage as ambiguous (8 bytes vs 8 GiB).
var unitQuantity = regexp.MustCompile(`^\d+(\.\d+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)$`)

// IsCPUQuantity reports whether s is a valid CPU quantity ("2", "1.5", "500m").
func IsCPUQuantity(s string) bool { return cpuQuantity.MatchString(s) }

// IsUnitQuantity reports whether s is a memory or storage quantity with a
// unit suffix ("8Gi", "512Mi", "50G").
func IsUnitQuantity(s string) bool { return unitQuantity.MatchString(s) }
