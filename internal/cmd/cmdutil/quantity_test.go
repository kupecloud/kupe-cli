package cmdutil

import (
	"errors"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

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

func TestValidateQuantities(t *testing.T) {
	flags := func(cpu, memory string, required bool) []QuantityFlag {
		return []QuantityFlag{
			{Name: "--cpu", Value: cpu, CPU: true, Required: required, Example: "500m"},
			{Name: "--memory", Value: memory, Required: required, Example: "1Gi"},
		}
	}
	long := strings.Repeat("1", 21)
	tests := []struct {
		name      string
		maxLength int
		flags     []QuantityFlag
		wantMsg   string
		wantHint  string
	}{
		{"all valid", 20, flags("500m", "1Gi", true), "", ""},
		{"optional flags may be empty", 20, flags("", "", false), "", ""},
		{"a missing required flag comes before a bad format", 20, flags("1Gi", "", true),
			"--memory is required", "example: --memory 1Gi"},
		{"a length over the cap comes before a bad format", 20, flags(long+"x", "1Gi", true),
			"--cpu is longer than 20 characters: " + long + "x", ""},
		{"no cap when maxLength is 0", 0, flags(long, "1Gi", true), "", ""},
		{"cpu with a unit", 20, flags("1Gi", "1Gi", true),
			"--cpu has an invalid format: 1Gi", "millicores (e.g. 500m), without a unit"},
		{"memory without a unit", 20, flags("1", "1024", false),
			"--memory must include a unit suffix; got: 1024", "example: --memory 1Gi (plain numbers are ambiguous)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuantities(tt.maxLength, tt.flags...)
			if tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ce *cli.Error
			if !errors.As(err, &ce) || ce.Code != cli.ExitMisuse {
				t.Fatalf("err = %v; want a misuse error", err)
			}
			if ce.Msg != tt.wantMsg {
				t.Fatalf("message = %q; want %q", ce.Msg, tt.wantMsg)
			}
			if !strings.Contains(ce.Hint, tt.wantHint) {
				t.Fatalf("hint = %q; want it to contain %q", ce.Hint, tt.wantHint)
			}
		})
	}
}
