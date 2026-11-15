package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Choosers are cancellable owned tasks. Form callbacks always run on the UI.
func (a *App) pickFile(title string, cb func(string)) {
	a.launch(func() {
		out, err := exec.CommandContext(a.appContext(), "zenity", "--file-selection", "--title="+title).Output()
		if err == nil {
			if p := strings.TrimSpace(string(out)); p != "" {
				a.onUI(func() { cb(p) })
			}
		}
	})
}
func (a *App) pickSaveFile(title, filename string, cb func(string)) {
	a.launch(func() {
		out, err := exec.CommandContext(a.appContext(), "zenity", "--file-selection", "--save", "--confirm-overwrite", "--title="+title, "--filename="+filename).Output()
		if err == nil {
			if p := strings.TrimSpace(string(out)); p != "" {
				a.onUI(func() { cb(p) })
			}
		}
	})
}

func (a *App) readQR(path string) {
	a.launch(func() {
		ctx, cancel := context.WithTimeout(a.appContext(), 15*time.Second)
		defer cancel()
		var s string
		var err error
		if path == "" {
			s, err = decodeQRClipboard(ctx)
		} else {
			s, err = decodeQRImage(ctx, path)
		}
		if err != nil {
			a.setMsg("%v", err)
			return
		}
		a.onUI(func() { a.applyQRCode(s) })
	})
}
func decodeQRImage(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "zbarimg", "-q", "--raw", path).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("没识别出二维码")
	}
	return strings.TrimSpace(string(out)), nil
}
func decodeQRClipboard(ctx context.Context) (string, error) {
	tmp, err := os.CreateTemp("", "clashgo-qr-*.png")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	cmd := exec.CommandContext(ctx, "wl-paste", "--type", "image/png")
	cmd.Stdout = tmp
	runErr := cmd.Run()
	tmp.Close()
	if st, err := os.Stat(tmp.Name()); runErr != nil || err != nil || st.Size() == 0 {
		return "", fmt.Errorf("剪贴板里没有图片")
	}
	return decodeQRImage(ctx, tmp.Name())
}
