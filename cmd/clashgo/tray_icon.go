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

// Both tray states use the same cat silhouette, shadow and alpha mask.
var inactiveTrayIcon = sync.OnceValue(func() []byte {
	data, err := grayscalePNG(assets.IconPNG)
	if err != nil {
		log.Printf("clashgo: tray grayscale: %v", err)
		return assets.IconPNG
	}
	return data
})

func trayIconPNG(active bool) []byte {
	if active {
		return assets.IconPNG
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
			value := uint8((299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B) + 500) / 1000)
			gray.SetNRGBA(x, y, color.NRGBA{R: value, G: value, B: value, A: c.A})
		}
	}
	var buf bytes.Buffer
	err = png.Encode(&buf, gray)
	return buf.Bytes(), err
}
