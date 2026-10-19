package ui

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

type rgb struct{ r, g, b float64 }

func hex(c lipgloss.Color) rgb {
	s := string(c)
	if len(s) != 7 || s[0] != '#' {
		return rgb{}
	}
	p := func(i int) float64 {
		n, _ := strconv.ParseUint(s[i:i+2], 16, 8)
		return float64(n)
	}
	return rgb{p(1), p(3), p(5)}
}

func (c rgb) String() string {
	return fmt.Sprintf("#%02X%02X%02X", int(c.r+0.5), int(c.g+0.5), int(c.b+0.5))
}

// gradient is the colour at t (0 to 1) along evenly spaced stops.
func gradient(stops []rgb, t float64) rgb {
	if t <= 0 || len(stops) == 1 {
		return stops[0]
	}
	if t >= 1 {
		return stops[len(stops)-1]
	}
	seg := t * float64(len(stops)-1)
	i := int(seg)
	f := seg - float64(i)
	a, b := stops[i], stops[i+1]
	return rgb{a.r + (b.r-a.r)*f, a.g + (b.g-a.g)*f, a.b + (b.b-a.b)*f}
}
