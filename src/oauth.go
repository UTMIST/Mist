package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/errors"
	"github.com/go-oauth2/oauth2/v4/generates"
	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/go-oauth2/oauth2/v4/store"
	oredis "github.com/go-oauth2/redis/v4"
	"github.com/go-redis/redis/v8"
)

type OAuthServer struct {
	Server  *server.Server
	Manager *manage.Manager
}

func NewOAuthServer(redisAddr string, client *redis.Client, log *slog.Logger) (*OAuthServer, error) {
	var tokenStore oauth2.TokenStore
	if redisAddr == "memory" {
		tokenStore, _ = store.NewMemoryTokenStore()
	} else {
		tokenStore = oredis.NewRedisStore(&redis.Options{Addr: redisAddr})
	}

	clientStore := store.NewClientStore()
	err := clientStore.Set("cli", &models.Client{
		ID:     "cli",
		Secret: "demo-client-secret",
		Domain: "http://localhost:3000",
	})
	if err != nil {
		return nil, err
	}

	manager := manage.NewDefaultManager()
	manager.MapTokenStorage(tokenStore)
	manager.MapClientStorage(clientStore)

	// Enable PKCE
	manager.SetValidateURIHandler(manage.DefaultValidateURI)
	manager.SetAuthorizeCodeExp(time.Minute * 10)

	manager.MapAccessGenerate(generates.NewAccessGenerate())

	srv := server.NewDefaultServer(manager)

	srv.SetAllowGetAccessRequest(true)
	srv.SetClientInfoHandler(server.ClientFormHandler)

	srv.SetUserAuthorizationHandler(func(w http.ResponseWriter, r *http.Request) (userID string, err error) {
		return "test-user", nil
	})

	srv.SetInternalErrorHandler(func(err error) (re *errors.Response) {
		log.Error("Internal OAuth2 error", "error", err)
		return
	})

	srv.SetResponseErrorHandler(func(re *errors.Response) {
		log.Error("OAuth2 response error", "error", re.Error)
	})

	return &OAuthServer{
		Server:  srv,
		Manager: manager,
	}, nil
}
