package exits

import (
	"fmt"
	"testing"
)

func TestForStatusMapping(t *testing.T) {
	cases := []struct {
		status int
		want   int
	}{
		{401, Auth},
		{403, Forbidden},
		{404, NotFound},
		{409, Conflict},
		{429, RateLimited},
		{503, Unavailable},
		{500, Failure},
		{418, Failure},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			if got := ForStatus(c.status); got != c.want {
				t.Fatalf("ForStatus(%d) = %d, want %d", c.status, got, c.want)
			}
		})
	}
}

func TestCodeValuesAreStable(t *testing.T) {
	cases := []struct {
		name      string
		got, want int
	}{
		{"OK", OK, 0},
		{"Failure", Failure, 1},
		{"Usage", Usage, 2},
		{"Auth", Auth, 3},
		{"Network", Network, 4},
		{"Forbidden", Forbidden, 5},
		{"NotFound", NotFound, 6},
		{"Conflict", Conflict, 7},
		{"RateLimited", RateLimited, 8},
		{"Unavailable", Unavailable, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Fatalf("%s = %d, want %d", c.name, c.got, c.want)
			}
		})
	}
}

func TestCode(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", New("boom", RateLimited))
	cases := map[string]struct {
		err  error
		want int
	}{
		"direct":  {New("x", Conflict), Conflict},
		"wrapped": {wrapped, RateLimited},
		"foreign": {fmt.Errorf("plain"), Failure},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Code(c.err); got != c.want {
				t.Fatalf("Code = %d, want %d", got, c.want)
			}
		})
	}
}
