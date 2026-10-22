// Package passwd 提供 Argon2id 口令哈希（alexedwards/argon2id）。
package passwd

import "github.com/alexedwards/argon2id"

// Hash 使用 Argon2id 哈希密码，返回 PHC 格式字符串。
func Hash(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}

// Verify 校验密码与 PHC 格式哈希。
func Verify(password, encoded string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, encoded)
}
