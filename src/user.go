package main

import (
	"context"
	"encoding/json"
	"github.com/teris-io/shortid"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *App) getUser(userID string) (*User, error) {
	ctx := context.Background()
	userData, err := a.redisClient.Get(ctx, "user:"+userID).Result()
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal([]byte(userData), &user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (a *App) getUserByEmail(email string) (*User, error) {
	ctx := context.Background()
	userID, err := a.redisClient.Get(ctx, "email:"+email).Result()
	if err != nil {
		return nil, err
	}
	return a.getUser(userID)
}

func (a *App) createUser(email, password string) (*User, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	userID, err := shortid.Generate()
	if err != nil {
		return nil, err
	}

	user := &User{
		ID:       userID,
		Email:    email,
		Password: string(hashedPassword),
	}

	userData, err := json.Marshal(user)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if err := a.redisClient.Set(ctx, "user:"+userID, userData, 0).Err(); err != nil {
		return nil, err
	}
	if err := a.redisClient.Set(ctx, "email:"+email, userID, 0).Err(); err != nil {
		return nil, err
	}

	return user, nil
}
