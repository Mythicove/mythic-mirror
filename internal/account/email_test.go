package account

import "testing"

func TestValidEmail(t *testing.T) {
	cases := map[string]bool{
		"a@b.co":                            true,
		"first.last+tag@sub.example.com":    true,
		"user@example.museum":               true,
		"a@b":                               false, // 无 TLD
		"a@b.":                              false,
		"@b.co":                             false,
		"a@":                                false,
		"a b@c.de":                          false, // 空格
		"a@b c.de":                          false,
		"a@@b.co":                           false,
		".a@b.co":                           true,  // 字符集允许（RFC 合法）
		string(make([]byte, 250)) + "@b.co": false, // 超长
	}
	for in, want := range cases {
		if got := validEmail(in); got != want {
			t.Errorf("validEmail(%q) = %v, want %v", in, got, want)
		}
	}
}
