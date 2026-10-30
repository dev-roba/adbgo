package adb

import "testing"

func TestIsMediaFile(t *testing.T) {
	yes := []string{"IMG_20240101.jpg", "photo.JPEG", "Screenshot_001.png", "clip.MP4", "raw.dng", "pic.heic", "vid.3gp"}
	no := []string{"notes.txt", "db.sqlite", "Makefile", ".gitignore"}
	for _, f := range yes {
		if !IsMediaFile(f) {
			t.Errorf("%q should be recognized as media", f)
		}
	}
	for _, f := range no {
		if IsMediaFile(f) {
			t.Errorf("%q should not be recognized as media", f)
		}
	}
}
