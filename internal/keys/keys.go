// Package keys 管理 Yggdrasil RSA 签名密钥对（ADR-0008）。
package keys

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// KeyPair 是 RSA 签名密钥对。
type KeyPair struct {
	priv *rsa.PrivateKey
}

// Generate 生成 RSA-2048 密钥对。
func Generate() (*KeyPair, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &KeyPair{priv: priv}, nil
}

// LoadOrGenerate 从文件加载 PEM 私钥，不存在则生成并持久化（0600）。
func LoadOrGenerate(path string) (*KeyPair, error) {
	if b, err := os.ReadFile(path); err == nil {
		return Parse(b)
	}
	kp, err := Generate()
	if err != nil {
		return nil, err
	}
	if err := kp.write(path); err != nil {
		return nil, err
	}
	return kp, nil
}

// Parse 解析 PEM 编码的 PKCS#8 RSA 私钥。
func Parse(pemBytes []byte) (*KeyPair, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("keys: failed to decode PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("keys: not an RSA private key")
	}
	return &KeyPair{priv: priv}, nil
}

func (k *KeyPair) write(path string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	der, err := x509.MarshalPKCS8PrivateKey(k.priv)
	if err != nil {
		return err
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}

// Sign 对 value 做 SHA256withRSA（PKCS#1 v1.5）签名，返回 base64。
func (k *KeyPair) Sign(value string) (string, error) {
	digest := sha256.Sum256([]byte(value))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k.priv, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// PublicKeyPEM 返回 base64 编码的公钥 PEM（供 meta 端点）。
func (k *KeyPair) PublicKeyPEM() string {
	der, err := x509.MarshalPKIXPublicKey(&k.priv.PublicKey)
	if err != nil {
		return ""
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(block))
}

// PublicKey 暴露底层公钥（供测试/校验）。
func (k *KeyPair) PublicKey() *rsa.PublicKey {
	return &k.priv.PublicKey
}

// PrivateKey 暴露底层私钥（供 OIDC JWT 签名）。
func (k *KeyPair) PrivateKey() *rsa.PrivateKey {
	return k.priv
}
