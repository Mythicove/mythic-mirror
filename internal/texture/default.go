package texture

import (
	"embed"
	"fmt"
	"image"
	"strings"
)

//go:embed builtin/wide/*.png builtin/slim/*.png
var builtinFS embed.FS

// BuiltinIDs 是 vanilla 内置默认皮肤（取自 1.20.1 客户端资源），wide/slim 两体型各一份。
var BuiltinIDs = []string{"steve", "alex", "ari", "efe", "kai", "makena", "noor", "sunny", "zuri"}

// BuiltinKey 是内置皮肤在 skin_key 中的引用形式：不进对象存储、不计配额、
// 服务端按 key 直接出图，故删除/配额/CDN 链路都不该把它当普通纹理。
func BuiltinKey(id, model string) string {
	return "builtin/" + widthOf(model) + "/" + id + ".png"
}

// widthOf 把玩家模型映射到内置资源目录（default→wide、slim→slim）。
func widthOf(model string) string {
	if model == "slim" {
		return "slim"
	}
	return "wide"
}

// IsBuiltin 判断 skin_key 是否为内置引用。
func IsBuiltin(key string) bool { return strings.HasPrefix(key, "builtin/") }

// BuiltinID 从内置 key 解析预设 id（builtin/wide/steve.png → steve）。
func BuiltinID(key string) (string, bool) {
	if !IsBuiltin(key) {
		return "", false
	}
	rest := strings.TrimPrefix(key, "builtin/")
	_, id, ok := strings.Cut(rest, "/")
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(id, ".png"), true
}

// BuiltinBytes 取内置皮肤原始 PNG。
func BuiltinBytes(key string) ([]byte, error) {
	if !IsBuiltin(key) {
		return nil, fmt.Errorf("texture: not a builtin key: %q", key)
	}
	b, err := builtinFS.ReadFile(key)
	if err != nil {
		return nil, fmt.Errorf("texture: builtin %q: %w", key, err)
	}
	return b, nil
}

// BuiltinImage 解码内置皮肤。
func BuiltinImage(key string) (image.Image, error) {
	b, err := BuiltinBytes(key)
	if err != nil {
		return nil, err
	}
	return Decode(b)
}

// DefaultSkin 是"未上传自定义皮肤"的回落：wide=steve、slim=alex（与 vanilla 一致）。
func DefaultSkin(slim bool) image.Image {
	key := BuiltinKey("steve", "default")
	if slim {
		key = BuiltinKey("alex", "slim")
	}
	img, err := BuiltinImage(key)
	if err != nil {
		// 内嵌资源缺失属构建事故：退回首帧全透明，绝不 panic
		return image.NewNRGBA(image.Rect(0, 0, 64, 64))
	}
	return img
}
