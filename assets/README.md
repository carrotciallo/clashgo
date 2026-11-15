# Assets

`icon.svg` is the original ClashGo mint-cat mark on the application's
lavender-blue brand color (`#8B9CF9`). The sidebar wordmark stays text-only.

`icon.png` is the committed 256px native tray asset embedded by `assets.go`;
normal offline Go builds do not require an SVG renderer. After editing the SVG,
regenerate the PNG with `go generate ./assets` from the repository root
(requires `rsvg-convert`).
