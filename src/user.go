package main

import (
	"context"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/go-redis/redis/v8"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Role         string `json:"role"`
}

type UserStore interface {
	Create(ctx context.Context, user *User) error
	GetByUsername(ctx context.Context, username string) (*User, error)
	VerifyPassword(user *User, password string) bool
}

type RedisUserStore struct {
	client *redis.Client
}

func NewRedisUserStore(client *redis.Client) *RedisUserStore {
	return &RedisUserStore{client: client}
}

func (s *RedisUserStore) Create(ctx context.Context, user *User) error {
	key := fmt.Sprintf("user:%s", user.Username)
	exists, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("user already exists")
	}

	data, err := json.Marshal(user)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, key, data, 0).Err()
}

func (s *RedisUserStore) GetByUsername(ctx context.Context, username string) (*User, error) {
	key := fmt.Sprintf("user:%s", username)
	val, err := s.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *RedisUserStore) VerifyPassword(user *User, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	return err == nil
}

func (u *User) ToSafe() map[string]interface{} {
	return map[string]interface{}{
		"id":       u.ID,
		"username": u.Username,
		"role":     u.Role,
	}
}
