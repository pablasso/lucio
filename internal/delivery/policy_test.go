package delivery

import "testing"

func TestParsePolicy(t *testing.T) {
	tests := []struct {
		raw  string
		want Policy
	}{
		{"", PolicyFull},
		{"full", PolicyFull},
		{"auto", PolicyAuto},
		{"partial", PolicyPartial},
	}
	for _, tt := range tests {
		got, err := ParsePolicy(tt.raw)
		if err != nil {
			t.Fatalf("ParsePolicy(%q) unexpected error: %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("ParsePolicy(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
	if _, err := ParsePolicy("sometimes"); err == nil {
		t.Fatal("expected invalid policy to fail")
	}
}
