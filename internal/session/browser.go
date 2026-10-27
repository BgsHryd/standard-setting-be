package session

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrBrowserSessionInvalid  = errors.New("browser session invalid")
	ErrBrowserSessionReplaced = errors.New("browser session replaced")
)

// ID is a SHA-256 digest of the cookie credential, never the credential itself.
type BrowserSession struct {
	// ExpectedID is used only when rotating an existing session.
	ExpectedID  string `json:"-"`
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	AuthVersion int    `json:"authVersion"`
	CSRFToken   string `json:"csrfToken"`
	ExpiresAt   int64  `json:"expiresAt"`
	Reason      string `json:"reason,omitempty"`
}

type BrowserStore interface {
	CreateBrowserSession(context.Context, BrowserSession) error
	GetBrowserSession(context.Context, string) (BrowserSession, error)
	RevokeBrowserSession(context.Context, string) error
	SubscribeBrowserSessions(context.Context, string) (<-chan struct{}, func(), error)
}

const browserPrefix = "ss:browser:session:"
const browserUserPrefix = "ss:browser:user:"
const browserEventsPrefix = "ss:browser:events:"

// All writes and publication are serialized by Redis. Old records keep their
// original expiry so a replaced device can receive the specific explanation.
var createBrowserSession = redis.NewScript(`
local previous = redis.call('GET', KEYS[2])
if ARGV[5] ~= '' and previous ~= ARGV[5] then return 0 end
local version = tonumber(redis.call('GET', KEYS[4]) or '-1')
if tonumber(ARGV[6]) < version then return 0 end
redis.call('SET', KEYS[4], ARGV[6])
redis.call('SET', KEYS[1], ARGV[1], 'PXAT', ARGV[3])
redis.call('SET', KEYS[2], ARGV[2], 'PXAT', ARGV[3])
if previous and previous ~= ARGV[2] then
 local oldkey = ARGV[4] .. previous
 local old = redis.call('GET', oldkey)
 if old then
  local record = cjson.decode(old)
  record.reason = 'replaced'
  redis.call('SET', oldkey, cjson.encode(record), 'KEEPTTL')
 end
end
redis.call('PUBLISH', KEYS[3], ARGV[2])
return 1
`)

func (r *Redis) CreateBrowserSession(ctx context.Context, rec BrowserSession) error {
	if rec.ID == "" || rec.UserID == "" || rec.CSRFToken == "" || rec.Reason != "" || rec.ExpiresAt <= time.Now().UnixMilli() {
		return ErrBrowserSessionInvalid
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	activated, err := createBrowserSession.Run(ctx, r.client, []string{browserPrefix + rec.ID, browserUserPrefix + rec.UserID, browserEventsPrefix + rec.UserID, "ss:browser:version:" + rec.UserID}, string(data), rec.ID, rec.ExpiresAt, browserPrefix, rec.ExpectedID, rec.AuthVersion).Int()
	if err != nil {
		return err
	}
	if activated == 0 {
		return ErrBrowserSessionReplaced
	}
	return nil
}

var getBrowserSession = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then return '' end
local record = cjson.decode(value)
if not record.reason and redis.call('GET', ARGV[1] .. record.userId) ~= record.id then
 record.reason = 'replaced'
 return cjson.encode(record)
end
return value
`)

func (r *Redis) GetBrowserSession(ctx context.Context, id string) (BrowserSession, error) {
	var rec BrowserSession
	value, err := getBrowserSession.Run(ctx, r.client, []string{browserPrefix + id}, browserUserPrefix).Text()
	if err != nil {
		return rec, err
	}
	if value == "" {
		return rec, ErrBrowserSessionInvalid
	}
	if err = json.Unmarshal([]byte(value), &rec); err != nil {
		return rec, err
	}
	if rec.ExpiresAt <= time.Now().UnixMilli() {
		return rec, ErrBrowserSessionInvalid
	}
	if rec.Reason == "replaced" {
		return rec, ErrBrowserSessionReplaced
	}
	if rec.Reason != "" {
		return rec, ErrBrowserSessionInvalid
	}
	return rec, nil
}

var revokeBrowserSession = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then return 0 end
local record = cjson.decode(value)
if record.reason then return 0 end
local userkey = ARGV[1] .. record.userId
if redis.call('GET', userkey) ~= record.id then return 0 end
redis.call('DEL', userkey)
record.reason = 'logout'
redis.call('SET', KEYS[1], cjson.encode(record), 'KEEPTTL')
redis.call('PUBLISH', ARGV[2] .. record.userId, record.id)
return 1
`)

func (r *Redis) RevokeBrowserSession(ctx context.Context, id string) error {
	return revokeBrowserSession.Run(ctx, r.client, []string{browserPrefix + id}, browserUserPrefix, browserEventsPrefix).Err()
}

func (r *Redis) SubscribeBrowserSessions(ctx context.Context, userID string) (<-chan struct{}, func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	pub := r.client.Subscribe(ctx, browserEventsPrefix+userID)
	if _, err := pub.Receive(ctx); err != nil {
		cancel()
		_ = pub.Close()
		return nil, nil, err
	}
	events := make(chan struct{}, 1)
	var once sync.Once
	closeSub := func() { once.Do(func() { cancel(); _ = pub.Close() }) }
	go func() {
		defer close(events)
		defer closeSub()
		messages := pub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-messages:
				if !ok {
					return
				}
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}
	}()
	return events, closeSub, nil
}
