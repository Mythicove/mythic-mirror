package texture

import (
	"image"
	"image/color"
)

// Face 渲染头部正面（8x8 单位），按 scale 放大。
func Face(img image.Image, scale int) *image.NRGBA {
	return region(img, 8, 8, 8, 8, scale)
}

// Body 渲染正面全身（16x32 单位），按 scale 放大。
func Body(img image.Image, scale int) *image.NRGBA {
	s := unit(img)
	factor := scale * s
	dst := image.NewNRGBA(image.Rect(0, 0, 16*factor, 32*factor))
	blit := func(sx, sy, dx, dy, w, h int) {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := at(img, sx*s+x*s, sy*s+y*s)
				for dy2 := 0; dy2 < factor; dy2++ {
					for dx2 := 0; dx2 < factor; dx2++ {
						dst.Set((dx+x)*factor+dx2, (dy+y)*factor+dy2, c)
					}
				}
			}
		}
	}
	blit(8, 8, 4, 0, 8, 8)     // head
	blit(20, 20, 4, 8, 8, 12)   // torso
	blit(44, 20, 0, 8, 4, 12)   // right arm
	blit(36, 52, 12, 8, 4, 12)  // left arm
	blit(4, 20, 4, 20, 4, 12)   // right leg
	blit(20, 52, 8, 20, 4, 12)  // left leg
	return dst
}

func region(img image.Image, sx, sy, w, h, scale int) *image.NRGBA {
	s := unit(img)
	factor := scale * s
	dst := image.NewNRGBA(image.Rect(0, 0, w*factor, h*factor))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := at(img, sx*s+x*s, sy*s+y*s)
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					dst.Set(x*factor+dx, y*factor+dy, c)
				}
			}
		}
	}
	return dst
}

func unit(img image.Image) int {
	s := img.Bounds().Dx() / 64
	if s < 1 {
		s = 1
	}
	return s
}

func at(img image.Image, x, y int) color.Color {
	b := img.Bounds()
	if x < b.Min.X {
		x = b.Min.X
	}
	if x >= b.Max.X {
		x = b.Max.X - 1
	}
	if y < b.Min.Y {
		y = b.Min.Y
	}
	if y >= b.Max.Y {
		y = b.Max.Y - 1
	}
	return img.At(x, y)
}
