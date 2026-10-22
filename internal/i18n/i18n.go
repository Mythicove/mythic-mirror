// Package i18n 提供基于 nicksnyder/go-i18n/v2 的 Localizer 实现。
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var files embed.FS

// Localizer 从内嵌 JSON 加载各 locale 消息。
type Localizer struct {
	mu     sync.RWMutex
	bundle *goi18n.Bundle
}

// New 加载全部 locale。
func New() (*Localizer, error) {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	entries, err := files.ReadDir("locales")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		b, err := files.ReadFile("locales/" + e.Name())
		if err != nil {
			return nil, err
		}
		if _, err := bundle.ParseMessageFileBytes(b, e.Name()); err != nil {
			return nil, err
		}
	}
	return &Localizer{bundle: bundle}, nil
}

// T 翻译消息键。
func (l *Localizer) T(_ context.Context, locale, key string, args map[string]any) string {
	loc := goi18n.NewLocalizer(l.bundle, locale)
	return loc.MustLocalize(&goi18n.LocalizeConfig{MessageID: key, TemplateData: args})
}

// Plural 处理复数形式。
func (l *Localizer) Plural(_ context.Context, locale, key string, n int, args map[string]any) string {
	loc := goi18n.NewLocalizer(l.bundle, locale)
	return loc.MustLocalize(&goi18n.LocalizeConfig{MessageID: key, TemplateData: args, PluralCount: n})
}
