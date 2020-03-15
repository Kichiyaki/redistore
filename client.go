package redistore

import (
	"github.com/go-redis/redis/v7"
)

type Client interface {
	Ping() *redis.StatusCmd
	Get(key string) *redis.StringCmd
	Do(args ...interface{}) *redis.Cmd
	Del(keys ...string) *redis.IntCmd
	Scan(cursor uint64, match string, count int64) *redis.ScanCmd
}
