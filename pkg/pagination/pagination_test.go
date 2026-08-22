package pagination

import "testing"

func TestNormalisePage(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{-5, DefaultPage},
		{0, DefaultPage},
		{1, 1},
		{9, 9},
	}
	for _, tc := range cases {
		if got := NormalisePage(tc.in); got != tc.want {
			t.Fatalf("NormalisePage(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNormalisePerPage(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, DefaultPerPage},
		{-1, DefaultPerPage},
		{5, 5},
		{MaxPerPage, MaxPerPage},
		{MaxPerPage + 1, MaxPerPage},
	}
	for _, tc := range cases {
		if got := NormalisePerPage(tc.in); got != tc.want {
			t.Fatalf("NormalisePerPage(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestOffset(t *testing.T) {
	cases := []struct {
		page, perPage, want int
	}{
		{0, 20, 0},
		{1, 20, 0},
		{2, 20, 20},
		{3, 15, 30},
	}
	for _, tc := range cases {
		if got := Offset(tc.page, tc.perPage); got != tc.want {
			t.Fatalf("Offset(%d, %d) = %d, want %d", tc.page, tc.perPage, got, tc.want)
		}
	}
}

func TestTotalPages(t *testing.T) {
	cases := []struct {
		total, perPage, want int
	}{
		{0, 20, 0},
		{1, 20, 1},
		{20, 20, 1},
		{21, 20, 2},
		{45, 20, 3},
		{45, 0, 3}, // an invalid page size falls back to the default
	}
	for _, tc := range cases {
		if got := TotalPages(tc.total, tc.perPage); got != tc.want {
			t.Fatalf("TotalPages(%d, %d) = %d, want %d", tc.total, tc.perPage, got, tc.want)
		}
	}
}
