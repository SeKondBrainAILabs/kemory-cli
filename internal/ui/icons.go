package ui

import (
	"embed"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// Each client's mark, white on its brand colour, cut to a 64px rounded tile.
// The marks come from Lobe Icons and Simple Icons; icons/NOTICE.md has their
// sources and licences.
//
//go:embed icons/*.png
var iconFiles embed.FS

// imageProtocol is how the terminal takes an inline image, if it does.
type imageProtocol int

const (
	noImages imageProtocol = iota
	itermImages
	kittyImages
)

// detectImages picks the protocol from the environment. Only terminals known
// to draw an image in its own cells get logos: iTerm2 and WezTerm (iTerm2's
// protocol), and kitty and Ghostty (kitty's). Warp draws images over the
// text, and tmux would need passthrough, so they get logos only with
// KEMORY_ICONS=1; KEMORY_ICONS=0 turns them off everywhere.
func detectImages(getenv func(string) string) imageProtocol {
	force := getenv("KEMORY_ICONS")
	if force == "0" {
		return noImages
	}
	term, program := getenv("TERM"), getenv("TERM_PROGRAM")
	switch {
	case getenv("TMUX") != "" && force != "1":
		return noImages
	case term == "xterm-kitty" || getenv("KITTY_WINDOW_ID") != "" || term == "xterm-ghostty" || program == "ghostty":
		return kittyImages
	case program == "iTerm.app" || program == "WezTerm" || force == "1":
		return itermImages
	}
	return noImages
}

// images is this terminal's protocol. A variable so tests can choose.
var images = detectImages(os.Getenv)

// Logo is host's logo drawn two cells wide and one row tall, or its
// monogram tile where the terminal cannot draw images.
func Logo(host string) string {
	png, err := iconFiles.ReadFile("icons/" + host + ".png")
	if err != nil || images == noImages {
		return tile(host)
	}
	return inlineImage(png, 2, 1)
}

// inlineImage draws png in a box cols wide and rows tall, in the terminal's
// protocol.
func inlineImage(png []byte, cols, rows int) string {
	data := base64.StdEncoding.EncodeToString(png)
	if images == itermImages {
		return fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1;size=%d:%s\a", cols, rows, len(png), data)
	}
	// kitty takes the image in chunks of at most 4096 base64 bytes.
	var b strings.Builder
	for first := true; data != ""; first = false {
		n := min(4096, len(data))
		chunk := data[:n]
		data = data[n:]
		more := 0
		if data != "" {
			more = 1
		}
		if first {
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,c=%d,r=%d,q=2,m=%d;%s\x1b\\", cols, rows, more, chunk)
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return b.String()
}
