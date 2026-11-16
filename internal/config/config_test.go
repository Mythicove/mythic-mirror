package config

import "testing"

// 选了驱动就必须配齐必需字段：半配置启动失败，不得静默禁用安全/外发控制。
func TestValidateCaptchaAndMail(t *testing.T) {
	empty := Config{}
	if err := empty.Validate(); err != nil {
		t.Fatalf("empty config must be valid: %v", err)
	}

	capPartial := Config{CaptchaDriver: "cap", CaptchaSiteKey: "k"}
	if err := capPartial.Validate(); err == nil {
		t.Fatal("cap without cap-url/secret must fail")
	}
	capOK := Config{CaptchaDriver: "cap", CaptchaCapURL: "https://cap.example", CaptchaSiteKey: "k", CaptchaSecret: "s"}
	if err := capOK.Validate(); err != nil {
		t.Fatalf("complete cap must pass: %v", err)
	}

	tsPartial := Config{CaptchaDriver: "turnstile", CaptchaSecret: "s"}
	if err := tsPartial.Validate(); err == nil {
		t.Fatal("turnstile without site key must fail")
	}
	unknownDriver := Config{CaptchaDriver: "nope"}
	if err := unknownDriver.Validate(); err == nil {
		t.Fatal("unknown captcha driver must fail")
	}

	smtpPartial := Config{MailDriver: "smtp", SMTPFrom: "a@b.c"}
	if err := smtpPartial.Validate(); err == nil {
		t.Fatal("smtp without addr must fail")
	}
	smtpOK := Config{MailDriver: "smtp", SMTPAddr: "smtp.example:465", SMTPFrom: "a@b.c"}
	if err := smtpOK.Validate(); err != nil {
		t.Fatalf("complete smtp must pass: %v", err)
	}
	resendPartial := Config{MailDriver: "resend"}
	if err := resendPartial.Validate(); err == nil {
		t.Fatal("resend without key/from must fail")
	}
	bogusMail := Config{MailDriver: "bogus"}
	if err := bogusMail.Validate(); err == nil {
		t.Fatal("unknown mail driver must fail")
	}
}
