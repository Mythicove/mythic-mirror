package keys

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
)

func TestSignVerify(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := kp.Sign("hello")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(sig)
	digest := sha256.Sum256([]byte("hello"))
	if err := rsa.VerifyPKCS1v15(kp.PublicKey(), crypto.SHA256, digest[:], raw); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

// signaturePublickey 的契约：字段值**本身就是 PEM 文本**，客户端（authlib-injector）直接解析。
// 曾经这里返回 base64(PEM)，注入端报 "Bad signature public key" 并放弃注入，客户端就认不到
// 本站皮肤——而这个形状是被旧单测固化下来的（它先解 base64 再解析，只能证明"我们自己解得开"）。
func TestPublicKeyPEM(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	text := kp.PublicKeyPEM()
	if !strings.HasPrefix(text, "-----BEGIN PUBLIC KEY-----") {
		t.Fatalf("signaturePublickey must be PEM text, got %.40q", text)
	}
	block, _ := pem.Decode([]byte(text))
	if block == nil || block.Type != "PUBLIC KEY" {
		t.Fatalf("bad public key PEM: %+v", block)
	}
	// 解出来的必须正是这把公钥，而不是"随便一段能解析的 PEM"
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !pub.(*rsa.PublicKey).Equal(kp.PublicKey()) {
		t.Fatal("PEM does not carry this key pair's public key")
	}
}
