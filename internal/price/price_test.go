package price

import (
	"errors"
	"testing"
)

func TestParseVND(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr error
	}{
		{in: "27,490,000₫", want: 27_490_000},
		{in: "27.490.000 đ", want: 27_490_000},
		{in: "27.490.000đ", want: 27_490_000},
		{in: "27490000", want: 27_490_000},
		{in: "27490000.00", want: 27_490_000},
		{in: "27490000.0", want: 27_490_000},
		{in: "Giá: 13.990.000 ₫", want: 13_990_000},
		{in: "  9.500.000 VND  ", want: 9_500_000},
		{in: "27.490.000đ 29.990.000đ", want: 27_490_000}, // sale price shown first
		{in: "Liên hệ", want: 0},
		{in: "", want: 0},
		{in: "100.000đ", wantErr: ErrOutOfRange},
		{in: "999.999.999đ", wantErr: ErrOutOfRange},
		{in: "0909123456789", wantErr: ErrOutOfRange},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseVND(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseVND(%q) error = %v, want %v", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVND(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseVND(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestParseVNDRangeBoundaries checks README §6: the sanity range is
// 500,000–200,000,000 VND inclusive.
func TestParseVNDRangeBoundaries(t *testing.T) {
	for _, tt := range []struct {
		in string
		ok bool
	}{
		{"500.000đ", true},
		{"499.999đ", false},
		{"200.000.000đ", true},
		{"200.000.001đ", false},
	} {
		_, err := ParseVND(tt.in)
		if (err == nil) != tt.ok {
			t.Errorf("ParseVND(%q) err = %v, want ok=%v", tt.in, err, tt.ok)
		}
	}
}

func TestFormatVND(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{27_490_000, "27.490.000₫"},
		{500_000, "500.000₫"},
		{1_000, "1.000₫"},
		{999, "999₫"},
		{-1_500_000, "-1.500.000₫"},
		{0, "liên hệ"},
	}
	for _, tt := range tests {
		if got := FormatVND(tt.in); got != tt.want {
			t.Errorf("FormatVND(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// Round trip with the parser.
	if v, err := ParseVND(FormatVND(27_490_000)); err != nil || v != 27_490_000 {
		t.Errorf("round trip = %d, %v", v, err)
	}
}
