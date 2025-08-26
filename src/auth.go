package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/teris-io/shortid"
)

// creates a user session in redis
func (a *App) CreateSession(uid string) (string, error) {
	id, err := shortid.Generate()
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	if err := a.redisClient.Set(ctx, "session:"+id, uid, 0).Err(); err != nil {
		return "", err
	}

	return id, nil
}

func (a *App) getSession(session_id string) (string, error) {
	ctx := context.Background()
	uid, err := a.redisClient.Get(ctx, "session:"+session_id).Result()
	if err != nil {
		return "", err
	}

	return uid, nil
}

func (a *App) getSessionFromRequest(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	log.Println("test")
	if strings.HasPrefix(authHeader, "Session ") {
		return strings.TrimPrefix(authHeader, "Session ")
	} else {
	if cookie, err := r.Cookie("session"); err == nil {
		return cookie.Value
	}
	}

	return ""
}

func (a *App) UserAuthorizationHandler(w http.ResponseWriter, r *http.Request) (string, error) {
	sessionID := a.getSessionFromRequest(r)

	if sessionID != "" {
		uid, err := a.getSession(sessionID)
		if err == nil {
			log.Println("authorized " + uid)
			return uid, nil
		}
		log.Printf("Session validation failed: %v", err)
	}

	return "", fmt.Errorf("not authenticated")
}

func (a *App) withAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")

		// Handle session-based auth
		if strings.HasPrefix(auth, "Session ") {
			sessionID := strings.TrimPrefix(auth, "Session ")
			userID, err := a.getSession(sessionID)
			if err != nil {
				a.jsonResponse(w, http.StatusUnauthorized, APIResponse{
					Success: false,
					Error:   "Invalid session",
				})
				return
			}

			ctx := context.WithValue(r.Context(), "user_id", userID)
			ctx = context.WithValue(ctx, "auth_type", "session")
			handler(w, r.WithContext(ctx))
			return
		}

		// Handle OAuth2 bearer tokens
		if strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			ti, err := a.manager.LoadAccessToken(context.Background(), token)
			if err != nil {
				a.jsonResponse(w, http.StatusUnauthorized, APIResponse{
					Success: false,
					Error:   "Invalid token",
				})
				return
			}

			ctx := context.WithValue(r.Context(), "user_id", ti.GetUserID())
			ctx = context.WithValue(ctx, "auth_type", "oauth2")
			ctx = context.WithValue(ctx, "client_id", ti.GetClientID())
			handler(w, r.WithContext(ctx))
			return
		}

		a.jsonResponse(w, http.StatusUnauthorized, APIResponse{
			Success: false,
			Error:   "Authentication required",
		})
	}
}
