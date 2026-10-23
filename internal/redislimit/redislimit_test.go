package redislimit

import (
	"context"
	"os"
	"testing"
	"time"
)

// 需要 REDIS_TEST_ADDR（如 localhost:6379）才会运行。
func testLimiter(t *testing.T, ns string) *Limiter {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	l, err := New(context.Background(), addr, ns)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestAllowFixedWindow(t *testing.T) {
	ctx := context.Background()
	l := testLimiter(t, "test")
	key := "ip-" + time.Now().Format("150405.000")

	for i := 0; i < 3; i++ {
		ok, err := l.Allow(ctx, key, 3, time.Minute)
		if err != nil || !ok {
			t.Fatalf("req %d: ok=%v err=%v", i, ok, err)
		}
	}
	ok, _ := l.Allow(ctx, key, 3, time.Minute)
	if ok {
		t.Fatal("expected limit exceeded")
	}
	// 其他 key 不受影响
	if ok, _ := l.Allow(ctx, key+"-other", 3, time.Minute); !ok {
		t.Fatal("other key should pass")
	}
}

func TestWindowExpiry(t *testing.T) {
	ctx := context.Background()
	l := testLimiter(t, "test-exp")
	key := "ip-" + time.Now().Format("150405.000")

	if ok, _ := l.Allow(ctx, key, 1, 50*time.Millisecond); !ok {
		t.Fatal("first should pass")
	}
	if ok, _ := l.Allow(ctx, key, 1, 50*time.Millisecond); ok {
		t.Fatal("second should be limited")
	}
	time.Sleep(80 * time.Millisecond)
	if ok, _ := l.Allow(ctx, key, 1, 50*time.Millisecond); !ok {
		t.Fatal("after window should pass")
	}
}
