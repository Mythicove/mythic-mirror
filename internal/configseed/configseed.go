// Package configseed 实现"部署期环境变量 → 数据库后台配置"的一次性交接。
//
// 背景：站点策略类开关（注册/验证/验证码范围等）的唯一来源是后台（settings KV）；
// 但存量部署的这些值原本写在环境变量里。升级时把旧 env 值播种进 DB 一次，
// 之后 env 对这些键彻底失效（启动期对仍被设置的废弃 env 打 WARN），
// 从根上消除"配置文件与后台谁覆盖谁"的困惑。
package configseed

import (
	"context"
	"strconv"
)

// Settings 是 configseed 需要的最小存储接口（yggdrasil.Store 满足）。
type Settings interface {
	GetSetting(ctx context.Context, key string) ([]byte, error)
	SetSetting(ctx context.Context, key string, value []byte) error
}

// MarkerKey 记录已完成的播种版本；存在即代表"交接已完成"，
// 也用于区分「从未播种」与「管理员故意删行重置」（后者不复活 env 值）。
const MarkerKey = "config_seed_version"

// Version 是当前播种逻辑版本；新增迁移项时递增，旧版本不重跑。
const Version = 1

// Item 是一个待播种的配置项。
type Item struct {
	// Key 是 settings KV 键。
	Key string
	// Envs 是该项关联的、交接后废弃的环境变量名（用于 stale 告警）。
	Envs []string
	// Build 从环境变量构造待播种值；ok=false 表示无值可播种（保持键缺失=读侧默认）。
	Build func(getenv func(string) string) (val []byte, ok bool)
}

// Run 执行版本化播种（调用方应在 advisory lock 内调用以保证多副本安全）。
// 返回实际播种的键与仍被设置的废弃 env 名（供告警）。幂等：marker 命中即跳过播种。
func Run(ctx context.Context, s Settings, version int, items []Item, getenv func(string) string) (seeded, stale []string, err error) {
	for _, it := range items {
		for _, e := range it.Envs {
			if getenv(e) != "" {
				stale = append(stale, e)
			}
		}
	}
	marker, _ := s.GetSetting(ctx, MarkerKey)
	if string(marker) == strconv.Itoa(version) {
		return nil, stale, nil
	}
	for _, it := range items {
		if _, gerr := s.GetSetting(ctx, it.Key); gerr == nil {
			continue // 已有值（管理员已设置或先前播种）：不覆盖
		}
		val, ok := it.Build(getenv)
		if !ok {
			continue
		}
		if err := s.SetSetting(ctx, it.Key, val); err != nil {
			return seeded, stale, err
		}
		seeded = append(seeded, it.Key)
	}
	if err := s.SetSetting(ctx, MarkerKey, []byte(strconv.Itoa(version))); err != nil {
		return seeded, stale, err
	}
	return seeded, stale, nil
}
