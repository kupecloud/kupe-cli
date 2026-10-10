package cmdutil

import "testing"

func TestQuantityValidation(t *testing.T) {
	tests := []struct {
		in       string
		cpu      bool
		withUnit bool
	}{
		{"2", true, false},
		{"1.5", true, false},
		{"500m", true, false},
		{"8Gi", false, true},
		{"512Mi", false, true},
		{"50G", false, true},
		{"8", true, false},
		{".5", true, false},
		{"1.5m", false, false},
		{"1.", false, false},
		{"m", false, false},
		{"", false, false},
		{"two", false, false},
		{"-1", false, false},
	}
	for _, tt := range tests {
		if got := IsCPUQuantity(tt.in); got != tt.cpu {
			t.Errorf("IsCPUQuantity(%q) = %v; want %v", tt.in, got, tt.cpu)
		}
		if got := IsUnitQuantity(tt.in); got != tt.withUnit {
			t.Errorf("IsUnitQuantity(%q) = %v; want %v", tt.in, got, tt.withUnit)
		}
	}
}
