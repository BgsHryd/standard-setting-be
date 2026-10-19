package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct{ client *redis.Client }

func NewRedis(address, password string, database int) *Redis {
	return &Redis{client: redis.NewClient(&redis.Options{Addr: address, Password: password, DB: database})}
}

func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

func (r *Redis) Close() error { return r.client.Close() }

func (r *Redis) Save(ctx context.Context, tokenID, userID string, ttl time.Duration) error {
	return r.client.Set(ctx, "ss:session:"+tokenID, userID, ttl).Err()
}

func (r *Redis) Consume(ctx context.Context, tokenID string) (string, error) {
	key := "ss:session:" + tokenID
	value, err := r.client.GetDel(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("consume session: %w", err)
	}
	return value, nil
}

func (r *Redis) GetCache(ctx context.Context, key string) ([]byte, error) {
	return r.client.Get(ctx, "ss:cache:"+key).Bytes()
}

func (r *Redis) SetCache(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, "ss:cache:"+key, value, ttl).Err()
}

func (r *Redis) DeleteCache(ctx context.Context, key string) error {
	return r.client.Del(ctx, "ss:cache:"+key).Err()
}

// Reserve both counters in a single Redis operation. Attempts have a fixed window:
// denied requests do not extend it, and a successful username reset preserves the IP spray guard.
var reserveLogin = redis.NewScript(`
local userCount = redis.call('INCR', KEYS[1])
if userCount == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
local ipCount = redis.call('INCR', KEYS[2])
if ipCount == 1 then redis.call('PEXPIRE', KEYS[2], ARGV[1]) end
local wait = 0
if userCount > tonumber(ARGV[2]) then wait = math.max(wait, redis.call('PTTL', KEYS[1])) end
if ipCount > tonumber(ARGV[3]) then wait = math.max(wait, redis.call('PTTL', KEYS[2])) end
return wait
`)

func loginKey(kind, value string) string {
	sum := sha256.Sum256([]byte(value))
	return "ss:login:" + kind + ":" + hex.EncodeToString(sum[:])
}
func (r *Redis) ReserveLogin(ctx context.Context, username, ip string) (time.Duration, error) {
	retry, err := reserveLogin.Run(ctx, r.client, []string{loginKey("user", username), loginKey("ip", ip)}, (15 * time.Minute).Milliseconds(), 5, 20).Int64()
	return time.Duration(retry) * time.Millisecond, err
}
func (r *Redis) ResetLogin(ctx context.Context, username string) error {
	return r.client.Del(ctx, loginKey("user", username)).Err()
}
