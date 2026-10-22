// Package i18n 提供基于内嵌 JSON 语言文件的 Localizer 实现（shared/i18n.Localizer）。
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed locales/*.json
var files embed.FS

// Localizer 从内嵌 JSON 加载各 locale 消息。
type Localizer struct {
	mu    sync.RWMutex
	dicts map[string]map[string]string
}

// New 加载全部 locale。
func New() (*Localizer, error) {
	l := &Localizer{dicts: map[string]map[string]string{}}
	entries, err := files.ReadDir("locales")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		b, err := files.ReadFile("locales/" + e.Name())
		if err != nil {
			return nil, err
		}
		var dict map[string]string
		if err := json.Unmarshal(b, &dict); err != nil {
			return nil, err
		}
		l.dicts[strings.TrimSuffix(e.Name(), ".json")] = dict
	}
	return l, nil
}

// T 翻译消息键。
func (l *Localizer) T(_ context.Context, locale, key string, args map[string]any) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	dict := l.dicts[locale]
	if dict == nil {
		dict = l.dicts["en-US"]
	}
	msg := dict[key]
	if msg == "" {
		msg = key
	}
	for k, v := range args {
		msg = strings.ReplaceAll(msg, "{"+k+"}", fmt.Sprint(v))
	}
	return msg
}

// Plural 处理复数形式（key.one / key.other）。
func (l *Localizer) Plural(_ context.Context, locale, key string, n int, args map[string]any) string {
	suffix := "other"
	if n == 1 {
		suffix = "one"
	}
	if args == nil {
		args = map[string]any{}
	}
	args["n"] = n
	return l.T(context.Background(), locale, key+"."+suffix, args)
}
