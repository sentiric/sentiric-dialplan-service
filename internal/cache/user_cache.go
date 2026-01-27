// sentiric-dialplan-service/internal/cache/user_cache.go
// ✅ YENİ: User Cache (Redis)

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
)

const UserCacheTTL = 5 * time.Minute

type UserCache struct {
	redis *redis.Client
}

func NewUserCache(redisClient *redis.Client) *UserCache {
	return &UserCache{redis: redisClient}
}

// GetUser attempts to fetch user from cache
// Returns nil if not found (cache miss)
func (c *UserCache) GetUser(ctx context.Context, phoneNumber string) (*userv1.User, error) {
	key := fmt.Sprintf("user:phone:%s", phoneNumber)
	
	val, err := c.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		// Cache miss
		return nil, nil
	}
	if err != nil {
		log.Error().Err(err).Str("phone", phoneNumber).Msg("Redis read error")
		return nil, err
	}
	
	var user userv1.User
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal cached user")
		return nil, err
	}
	
	log.Debug().Str("phone", phoneNumber).Msg("✅ Cache HIT")
	return &user, nil
}

// SetUser stores user in cache with TTL
func (c *UserCache) SetUser(ctx context.Context, phoneNumber string, user *userv1.User) error {
	key := fmt.Sprintf("user:phone:%s", phoneNumber)
	
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}
	
	if err := c.redis.Set(ctx, key, data, UserCacheTTL).Err(); err != nil {
		log.Error().Err(err).Str("phone", phoneNumber).Msg("Redis write error")
		return err
	}
	
	log.Debug().Str("phone", phoneNumber).Dur("ttl", UserCacheTTL).Msg("✅ User cached")
	return nil
}

// InvalidateUser removes user from cache
func (c *UserCache) InvalidateUser(ctx context.Context, phoneNumber string) error {
	key := fmt.Sprintf("user:phone:%s", phoneNumber)
	return c.redis.Del(ctx, key).Err()
}
