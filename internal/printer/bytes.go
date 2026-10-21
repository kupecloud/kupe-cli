package printer

import (
	"strconv"
	"strings"
)

// binaryUnits are the units humanBytes steps through, each 1024 times the
// one before.
var binaryUnits = []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// humanBytes renders a byte count in binary units with at most one
// decimal: "512 B", "1.5 KiB", "3.2 GiB", "5 GiB". A value that rounds up
// to 1024 of a unit is shown as 1 of the next.
func humanBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	v := float64(n) / 1024
	i := 0
	for v >= 1024 && i < len(binaryUnits)-1 {
		v /= 1024
		i++
	}
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if s == "1024.0" && i < len(binaryUnits)-1 {
		s, i = "1.0", i+1
	}
	return strings.TrimSuffix(s, ".0") + " " + binaryUnits[i]
}
