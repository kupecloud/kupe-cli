package printer

import (
	"math"
	"testing"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1 KiB"},
		{1536, "1.5 KiB"},
		{1024*1024 - 1, "1 MiB"}, // 1023.999 KiB rounds up to the next unit
		{1024 * 1024, "1 MiB"},
		{10 * 1024 * 1024, "10 MiB"},
		{3435973837, "3.2 GiB"},
		{5368709120, "5 GiB"},
		{1024 * 1024 * 1024 * 1024, "1 TiB"},
		{1536 * 1024 * 1024 * 1024 * 1024, "1.5 PiB"},
		{math.MaxInt64, "8 EiB"},
	}
	for _, tt := range tests {
		if got := humanBytes(tt.n); got != tt.want {
			t.Errorf("humanBytes(%d) = %q; want %q", tt.n, got, tt.want)
		}
	}
}
