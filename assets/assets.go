package assets

import _ "embed"

// IconPNG is the full-color application/window mark, separate from the tray.
// Keep icon.svg as the editable source; regenerate with go generate ./assets.
//
//go:generate rsvg-convert -w 256 -h 256 -o icon.png icon.svg
//go:embed icon.png
var IconPNG []byte

// TrayPNG is the transparent outlined cat for small native tray slots.
//
//go:generate rsvg-convert -w 256 -h 256 -o tray.png tray.svg
//go:embed tray.png
var TrayPNG []byte
