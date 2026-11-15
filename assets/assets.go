package assets

import _ "embed"

// IconPNG is the rasterized application mark for native tray backends.
// Keep icon.svg as the editable source; regenerate with go generate ./assets.
//
//go:generate rsvg-convert -w 256 -h 256 -o icon.png icon.svg
//go:embed icon.png
var IconPNG []byte
