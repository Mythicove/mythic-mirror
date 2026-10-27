package account

import (
	"context"
	"slices"
	"strings"
)

// RegistrationPolicy 是注册与验证的站点策略，**唯一来源是后台（settings KV）**。
// 部署期环境变量只在升级时经 configseed 一次性播种进 DB，之后不再读取，
// 因此不存在"配置文件与后台谁覆盖谁"的疑问。
type RegistrationPolicy struct {
	DisableRegistration  bool     `json:"disableRegistration"`
	InviteOnly           bool     `json:"inviteOnly"`
	EmailDomainWhitelist []string `json:"emailDomainWhitelist"`
	// CaptchaScopes 是验证码保护范围（KnownCaptchaScopes 子集；nil=默认仅 register）。
	CaptchaScopes []string `json:"captchaScopes"`
}

// PolicySource 提供生效中的注册策略，由 admin.Service 实现。
type PolicySource interface {
	RegistrationPolicy(ctx context.Context) (RegistrationPolicy, error)
}

// NormalizeRegistrationPolicy 归一化：scope 去空白/剔除未登记值；nil scopes=默认仅 register。
// 读取侧（account）与写入侧（admin）共用同一归一化，避免两处规则漂移。
func NormalizeRegistrationPolicy(p RegistrationPolicy) RegistrationPolicy {
	if p.CaptchaScopes == nil {
		p.CaptchaScopes = slices.Clone(DefaultCaptchaScopes)
	} else {
		known := make([]string, 0, len(p.CaptchaScopes))
		for _, sc := range p.CaptchaScopes {
			sc = strings.TrimSpace(sc)
			if sc == "" || !slices.Contains(KnownCaptchaScopes, sc) {
				continue
			}
			known = append(known, sc)
		}
		p.CaptchaScopes = known
	}
	if p.EmailDomainWhitelist == nil {
		p.EmailDomainWhitelist = []string{}
	}
	return p
}

// regPolicy 读取生效策略；未注入来源时返回归一化零值（仅测试/嵌入场景）。
func (s *Service) regPolicy(ctx context.Context) RegistrationPolicy {
	if s.pol == nil {
		return NormalizeRegistrationPolicy(RegistrationPolicy{})
	}
	p, err := s.pol.RegistrationPolicy(ctx)
	if err != nil {
		return NormalizeRegistrationPolicy(RegistrationPolicy{})
	}
	return NormalizeRegistrationPolicy(p)
}
