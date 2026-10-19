package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// mark is the SeKondBrain mark, a ring open at the top left with three dots
// in the gap around a disc, redrawn on a 14 by 14 grid so that the ring and
// dots survive at terminal size. Each value is the colour of the original
// gradient at that point, or 0 where the pixel is off.
var mark = [14][14]uint32{
	{0, 0, 0, 0, 0, 0, 0x7EA0FC, 0x71A0FC, 0x65A0FC, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0x9C98FC, 0, 0, 0, 0x459CF8, 0x479DFB, 0x5AA1FC, 0, 0, 0},
	{0, 0, 0xC384FC, 0, 0, 0, 0, 0, 0, 0, 0x499EFA, 0x6A9FFD, 0, 0},
	{0, 0xD77BFD, 0, 0, 0, 0, 0x5B8FF1, 0x4092EF, 0, 0, 0, 0x6499F9, 0x819DFE, 0},
	{0, 0, 0, 0, 0x9A88F2, 0x7E89F0, 0x658BED, 0x4E8CEC, 0x418FEE, 0x4493F1, 0, 0, 0x8898FD, 0},
	{0, 0, 0, 0, 0xA382F1, 0x8A84EC, 0x7383E9, 0x6487EB, 0x5C8AEC, 0x5C8CEE, 0, 0, 0x9495FB, 0xAC98FF},
	{0xF868FC, 0, 0, 0xBF73F4, 0xAC79F0, 0x997DEB, 0x867EE6, 0x7A81E8, 0x7586EB, 0x7688ED, 0x818DF3, 0, 0, 0xB790FE},
	{0xF875DD, 0, 0, 0xCA6AF2, 0xB86FEE, 0xA974EC, 0x9C78E8, 0x937CE8, 0x9081EB, 0x9486EF, 0x9B89F3, 0, 0, 0xC08AFF},
	{0xFA82C3, 0xF66DDF, 0, 0, 0xC866F0, 0xBC6CEE, 0xB070EC, 0xA873EB, 0xA678ED, 0xA97DF1, 0, 0, 0xC380FC, 0xCC82FF},
	{0, 0xFB84B1, 0, 0, 0xDC60F4, 0xCD63F0, 0xC466EE, 0xBF6BEF, 0xBD6FF1, 0xBC71F1, 0, 0, 0xD373FB, 0},
	{0, 0xFE9A8D, 0xFA86AD, 0, 0, 0, 0xDB5FF2, 0xD462F2, 0, 0, 0, 0xDE6DFA, 0xE36EFD, 0},
	{0, 0, 0xFFA478, 0xFA8E9E, 0, 0, 0, 0, 0, 0, 0xEC63FA, 0xF162FC, 0, 0},
	{0, 0, 0, 0xFFAA6D, 0xFE9C82, 0xF98D9D, 0, 0, 0xF775D0, 0xF670D3, 0xF871DC, 0, 0, 0},
	{0, 0, 0, 0, 0, 0xFDA673, 0xFEA07F, 0xFE9B8D, 0xFC929D, 0, 0, 0, 0, 0},
}

// markRows is how many terminal rows the mark takes: two pixels per row.
const markRows = len(mark) / 2

func colourOf(rgb uint32) lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%06X", rgb))
}

// markLines draws the mark with half blocks: the upper pixel of each cell is
// the foreground of ▀, the lower one its background.
func markLines() []string {
	lines := make([]string, 0, markRows)
	for y := 0; y < len(mark); y += 2 {
		var b strings.Builder
		for x := range mark[y] {
			top, bottom := mark[y][x], mark[y+1][x]
			switch {
			case top != 0 && bottom != 0:
				b.WriteString(lipgloss.NewStyle().Foreground(colourOf(top)).Background(colourOf(bottom)).Render("▀"))
			case top != 0:
				b.WriteString(lipgloss.NewStyle().Foreground(colourOf(top)).Render("▀"))
			case bottom != 0:
				b.WriteString(lipgloss.NewStyle().Foreground(colourOf(bottom)).Render("▄"))
			default:
				b.WriteString(" ")
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}
