package texture

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func solidPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestValidateSkin(t *testing.T) {
	valid := []struct{ w, h int }{{64, 64}, {64, 32}, {128, 128}, {128, 256}, {256, 256}}
	for _, c := range valid {
		if _, _, _, err := ValidateSkin(solidPNG(c.w, c.h)); err != nil {
			t.Errorf("expected valid %dx%d, got %v", c.w, c.h, err)
		}
	}
	invalid := []struct{ w, h int }{{63, 63}, {64, 48}, {100, 100}, {64, 63}}
	for _, c := range invalid {
		if _, _, _, err := ValidateSkin(solidPNG(c.w, c.h)); err == nil {
			t.Errorf("expected invalid %dx%d", c.w, c.h)
		}
	}
}

func TestValidateCape(t *testing.T) {
	if _, _, _, err := ValidateCape(solidPNG(64, 32)); err != nil {
		t.Errorf("cape 64x32 should be valid: %v", err)
	}
	if _, _, _, err := ValidateCape(solidPNG(64, 64)); err == nil {
		t.Errorf("cape 64x64 should be invalid")
	}
}

func TestFaceAndBody(t *testing.T) {
	img, err := Decode(solidPNG(64, 64))
	if err != nil {
		t.Fatal(err)
	}
	face := Face(img, 2)
	if face.Bounds().Dx() != 16 || face.Bounds().Dy() != 16 {
		t.Fatalf("face size %v", face.Bounds())
	}
	body := Body(img, 2)
	if body.Bounds().Dx() != 32 || body.Bounds().Dy() != 64 {
		t.Fatalf("body size %v", body.Bounds())
	}
}
