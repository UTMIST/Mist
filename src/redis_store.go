package main

import (
	"context"
	"encoding/json"

	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/redis/go-redis/v9"
)

type RedisTokenStore struct {
	client *redis.Client
}

func NewRedisTokenStore(client *redis.Client) *RedisTokenStore {
	return &RedisTokenStore{
		client: client,
	}
}

func (s *RedisTokenStore) Create(ctx context.Context, info oauth2.TokenInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()
	if code := info.GetCode(); code != "" {
		pipe.Set(ctx, "oauth:code:"+code, data, info.GetCodeExpiresIn())
	}
	if access := info.GetAccess(); access != "" {
		pipe.Set(ctx, "oauth:access:"+access, data, info.GetAccessExpiresIn())
	}
	if refresh := info.GetRefresh(); refresh != "" {
		pipe.Set(ctx, "oauth:refresh:"+refresh, data, info.GetRefreshExpiresIn())
	}

	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisTokenStore) RemoveByCode(ctx context.Context, code string) error {
	return s.client.Del(ctx, "oauth:code:"+code).Err()
}

func (s *RedisTokenStore) RemoveByAccess(ctx context.Context, access string) error {
	return s.client.Del(ctx, "oauth:access:"+access).Err()
}

func (s *RedisTokenStore) RemoveByRefresh(ctx context.Context, refresh string) error {
	return s.client.Del(ctx, "oauth:refresh:"+refresh).Err()
}

func (s *RedisTokenStore) GetByCode(ctx context.Context, code string) (oauth2.TokenInfo, error) {
	return s.get(ctx, "oauth:code:"+code)
}

func (s *RedisTokenStore) GetByAccess(ctx context.Context, access string) (oauth2.TokenInfo, error) {
	return s.get(ctx, "oauth:access:"+access)
}

func (s *RedisTokenStore) GetByRefresh(ctx context.Context, refresh string) (oauth2.TokenInfo, error) {
	return s.get(ctx, "oauth:refresh:"+refresh)
}

func (s *RedisTokenStore) get(ctx context.Context, key string) (oauth2.TokenInfo, error) {
	result, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var tm models.Token
	if err := json.Unmarshal([]byte(result), &tm); err != nil {
		return nil, err
	}
	return &tm, nil
}
