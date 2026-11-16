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
	// Password 是密码复杂度策略；零值经归一化后为默认（仅长度>=MinPasswordLength）。
	Password PasswordPolicy `json:"password"`
	// AllowUnverifiedLogin 放行"未验证邮箱"登录（默认 false=注册必须完成邮件验证）。
	// 用反向字段是因为零值必须等于安全默认（要求验证），旧存储缺该键时行为即安全侧。
	AllowUnverifiedLogin bool `json:"allowUnverifiedLogin"`
}

// PasswordPolicy 是密码复杂度要求。MinLength 恒生效（下限 MinPasswordLength），
// 其余四项为可选开关。前端实时清单与后端校验共用 Evaluate/Check，规则单点。
type PasswordPolicy struct {
	MinLength     int  `json:"minLength"`
	RequireUpper  bool `json:"requireUpper"`
	RequireLower  bool `json:"requireLower"`
	RequireDigit  bool `json:"requireDigit"`
	RequireSymbol bool `json:"requireSymbol"`
}

// MaxPasswordLength 是复杂度可配置的最大下限，防后台误配出无法满足的策略。
const MaxPasswordLength = 64

// PasswordChecks 是逐条要求的满足情况，键与前端清单一一对应。
type PasswordChecks struct {
	Length bool `json:"length"`
	Upper  bool `json:"upper"`
	Lower  bool `json:"lower"`
	Digit  bool `json:"digit"`
	Symbol bool `json:"symbol"`
}

// Evaluate 逐条判定密码是否满足各要求（未启用的要求也给出判定，供前端灰显）。
func (p PasswordPolicy) Evaluate(pw string) PasswordChecks {
	c := PasswordChecks{Length: len(pw) >= p.MinLength}
	for _, r := range pw {
		switch {
		case r >= 'A' && r <= 'Z':
			c.Upper = true
		case r >= 'a' && r <= 'z':
			c.Lower = true
		case r >= '0' && r <= '9':
			c.Digit = true
		default:
			c.Symbol = true
		}
	}
	return c
}

// Check 按**已启用**的要求判定；任一不满足返回 ErrWeakPassword。
func (p PasswordPolicy) Check(pw string) error {
	c := p.Evaluate(pw)
	if !c.Length ||
		(p.RequireUpper && !c.Upper) ||
		(p.RequireLower && !c.Lower) ||
		(p.RequireDigit && !c.Digit) ||
		(p.RequireSymbol && !c.Symbol) {
		return ErrWeakPassword
	}
	return nil
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
	// 长度下限夹在 [MinPasswordLength, MaxPasswordLength]：0/负=默认，过大=误配兜底。
	if p.Password.MinLength < MinPasswordLength {
		p.Password.MinLength = MinPasswordLength
	}
	if p.Password.MinLength > MaxPasswordLength {
		p.Password.MinLength = MaxPasswordLength
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
