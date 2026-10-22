package keys

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
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

func TestPublicKeyPEM(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	pemB64 := kp.PublicKeyPEM()
	pemBytes, err := base64.StdEncoding.DecodeString(pemB64)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PUBLIC KEY" {
		t.Fatalf("bad public key PEM: %+v", block)
	}
}
