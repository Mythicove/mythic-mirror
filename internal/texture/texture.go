// Package texture 负责皮肤/披风 PNG 的校验、归一化与 2D 渲染。
package texture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
)

// MaxTextureBytes 是纹理上传的最大字节数（4 MiB）。
const MaxTextureBytes = 4 << 20

var (
	ErrInvalidSkin = errors.New("invalid skin dimensions")
	ErrInvalidCape = errors.New("invalid cape dimensions")
	ErrTooLarge    = errors.New("texture too large")
)

// ValidateSkin 校验并归一化皮肤 PNG，返回归一化字节与尺寸。
func ValidateSkin(data []byte) ([]byte, int, int, error) {
	img, w, h, err := decodePNG(data)
	if err != nil {
		return nil, 0, 0, err
	}
	if !validSkin(w, h) {
		return nil, 0, 0, fmt.Errorf("%w: %dx%d", ErrInvalidSkin, w, h)
	}
	return normalize(img), w, h, nil
}

// ValidateCape 校验并归一化披风 PNG（固定 64x32）。
func ValidateCape(data []byte) ([]byte, int, int, error) {
	img, w, h, err := decodePNG(data)
	if err != nil {
		return nil, 0, 0, err
	}
	if w != 64 || h != 32 {
		return nil, 0, 0, fmt.Errorf("%w: %dx%d", ErrInvalidCape, w, h)
	}
	return normalize(img), w, h, nil
}

// ReadAllLimit 受限读取，超过 limit 返回 ErrTooLarge。
func ReadAllLimit(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Decode 解码 PNG 为 image.Image（供渲染）。
func Decode(data []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(data))
}

func decodePNG(data []byte) (image.Image, int, int, error) {
	if len(data) > MaxTextureBytes {
		return nil, 0, 0, ErrTooLarge
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	return img, b.Dx(), b.Dy(), nil
}

func normalize(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// validSkin：宽为 2 的幂且 >=64；高为 32（仅 64x32 legacy）、等于宽、或 2 倍宽（HD 第二层）。
func validSkin(w, h int) bool {
	if w < 64 || w&(w-1) != 0 {
		return false
	}
	if w == 64 && h == 32 {
		return true
	}
	return h == w || h == 2*w
}
