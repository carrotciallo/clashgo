package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"
	"sync"

	"github.com/carrotciallo/clashgo/assets"
)

// Both tray states use the same line art and alpha mask; only ink changes.
var inactiveTrayIcon = sync.OnceValue(func() []byte {
	data, err := grayscalePNG(assets.TrayPNG)
	if err != nil {
		log.Printf("clashgo: tray grayscale: %v", err)
		return assets.TrayPNG
	}
	return data
})

func trayIconPNG(active bool) []byte {
	if active {
		return assets.TrayPNG
	}
	return inactiveTrayIcon()
}

func grayscalePNG(data []byte) ([]byte, error) {
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	gray := image.NewNRGBA(source.Bounds())
	for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
		for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
			c := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			gray.SetNRGBA(x, y, color.NRGBA{R: 140, G: 140, B: 140, A: c.A})
		}
	}
	var buf bytes.Buffer
	err = png.Encode(&buf, gray)
	return buf.Bytes(), err
}
