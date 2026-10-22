package passwd

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := Hash("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := Verify("s3cret", h)
	if err != nil || !ok {
		t.Fatalf("verify ok=%v err=%v", ok, err)
	}
	if ok, _ := Verify("wrong", h); ok {
		t.Fatal("wrong password should fail")
	}
}
