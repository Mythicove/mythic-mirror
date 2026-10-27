package configseed

import (
	"context"
	"errors"
	"testing"
)

type memSettings map[string][]byte

func (m memSettings) GetSetting(_ context.Context, k string) ([]byte, error) {
	v, ok := m[k]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m memSettings) SetSetting(_ context.Context, k string, v []byte) error {
	m[k] = v
	return nil
}

func TestRunSeedsOnceAndWarnsStale(t *testing.T) {
	ctx := context.Background()
	s := memSettings{}
	getenv := func(k string) string {
		if k == "E" {
			return "x"
		}
		return ""
	}
	items := []Item{{
		Key:  "k",
		Envs: []string{"E"},
		Build: func(g func(string) string) ([]byte, bool) {
			if g("E") == "" {
				return nil, false
			}
			return []byte("seeded"), true
		},
	}}

	seeded, stale, err := Run(ctx, s, 1, items, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeded) != 1 || seeded[0] != "k" || string(s["k"]) != "seeded" {
		t.Fatalf("first run should seed: %v %q", seeded, s["k"])
	}
	if len(stale) != 1 || stale[0] != "E" {
		t.Fatalf("stale env should be reported: %v", stale)
	}

	// 二次运行：marker 命中，不再播种，但仍报 stale
	seeded, stale, err = Run(ctx, s, 1, items, getenv)
	if err != nil || len(seeded) != 0 || len(stale) != 1 {
		t.Fatalf("second run: seeded=%v stale=%v err=%v", seeded, stale, err)
	}

	// 管理员删行重置后，seed 不复活 env 值
	delete(s, "k")
	if _, _, err := Run(ctx, s, 1, items, getenv); err != nil {
		t.Fatal(err)
	}
	if _, exists := s["k"]; exists {
		t.Fatal("admin reset must not be re-seeded from env")
	}
}

func TestRunNoEnvLeavesKeyAbsent(t *testing.T) {
	ctx := context.Background()
	s := memSettings{}
	items := []Item{{
		Key:  "k",
		Envs: []string{"E"},
		Build: func(g func(string) string) ([]byte, bool) {
			if g("E") == "" {
				return nil, false
			}
			return []byte("x"), true
		},
	}}
	seeded, stale, err := Run(ctx, s, 1, items, func(string) string { return "" })
	if err != nil || len(seeded) != 0 || len(stale) != 0 {
		t.Fatalf("seeded=%v stale=%v err=%v", seeded, stale, err)
	}
	if _, exists := s["k"]; exists {
		t.Fatal("no env value must leave key absent (read-side default)")
	}
	if string(s[MarkerKey]) != "1" {
		t.Fatalf("marker should be written: %q", s[MarkerKey])
	}
}
