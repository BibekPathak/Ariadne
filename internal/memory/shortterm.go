package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const shortTermKeyPrefix = "agent:st:"

// ShortTermRedis stores ephemeral per-agent memory as a JSON list with a TTL.
// A write refreshes the whole key's TTL.
type ShortTermRedis struct {
	client *redis.Client
	ttl    time.Duration
}

func NewShortTermRedis(addr string, ttl time.Duration) (*ShortTermRedis, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &ShortTermRedis{client: client, ttl: ttl}, nil
}

func (s *ShortTermRedis) key(agentID string) string { return shortTermKeyPrefix + agentID }

func (s *ShortTermRedis) StoreShortTerm(ctx context.Context, agentID string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	// LPUSH-like semantics: newer entries are prepended to existing ones.
	existing, _ := s.client.LRange(ctx, s.key(agentID), 0, -1).Result()
	if len(existing) > 0 {
		var cur []Entry
		_ = json.Unmarshal([]byte(existing[0]), &cur)
		cur = append(entries, cur...)
		if len(cur) > 200 {
			cur = cur[:200]
		}
		raw, _ = json.Marshal(cur)
	}
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, s.key(agentID))
	pipe.RPush(ctx, s.key(agentID), raw)
	pipe.Expire(ctx, s.key(agentID), s.ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *ShortTermRedis) LoadShortTerm(ctx context.Context, agentID string, limit int) ([]Entry, error) {
	raw, err := s.client.LRange(ctx, s.key(agentID), 0, int64(limit-1)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, r := range raw {
		var entries []Entry
		if err := json.Unmarshal([]byte(r), &entries); err == nil {
			out = append(out, entries...)
		}
	}
	return out, nil
}

func (s *ShortTermRedis) Close() error { return s.client.Close() }
