package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// pickFile opens the desktop file chooser (zenity) and calls back
// with the chosen path. The chooser runs on its own goroutine; the
// callback only stashes the path into form state, which the
// continuously redrawn UI picks up on its next frame.
func pickFile(title string, cb func(path string)) {
	go func() {
		out, err := exec.Command("zenity", "--file-selection", "--title="+title).Output()
		if err != nil {
			return // cancelled or no chooser available
		}
		if p := strings.TrimSpace(string(out)); p != "" {
			cb(p)
		}
	}()
}

// pickSaveFile opens the desktop save chooser (zenity) seeded
// with a filename and calls back with the chosen destination.
func pickSaveFile(title, filename string, cb func(path string)) {
	go func() {
		out, err := exec.Command("zenity", "--file-selection", "--save", "--confirm-overwrite",
			"--title="+title, "--filename="+filename).Output()
		if err != nil {
			return // cancelled or no chooser available
		}
		if p := strings.TrimSpace(string(out)); p != "" {
			cb(p)
		}
	}()
}

// decodeQRImage runs zbarimg over an image file and returns the
// first decoded payload.
func decodeQRImage(path string) (string, error) {
	out, err := exec.Command("zbarimg", "-q", "--raw", path).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("没识别出二维码")
	}
	return strings.TrimSpace(string(out)), nil
}

// decodeQRClipboard pulls an image off the Wayland clipboard and
// decodes the QR code in it.
func decodeQRClipboard() (string, error) {
	tmp, err := os.CreateTemp("", "clashgo-qr-*.png")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	cmd := exec.Command("wl-paste", "--type", "image/png")
	cmd.Stdout = tmp
	runErr := cmd.Run()
	tmp.Close()
	if st, serr := os.Stat(tmp.Name()); runErr != nil || serr != nil || st.Size() == 0 {
		return "", fmt.Errorf("剪贴板里没有图片")
	}
	return decodeQRImage(tmp.Name())
}
