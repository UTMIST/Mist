package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/go-oauth2/oauth2/v4/store"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type App struct {
	redisClient *redis.Client
	scheduler   *Scheduler
	httpServer  *http.Server
	manager     *manage.Manager
	srv         *server.Server
	wg          sync.WaitGroup
}

func NewApp(redisAddr, gpuType string) *App {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	scheduler := NewScheduler(redisAddr)

	manager := manage.NewDefaultManager()
	manager.SetAuthorizeCodeTokenCfg(manage.DefaultAuthorizeCodeTokenCfg)
	manager.MustTokenStorage(store.NewMemoryTokenStore()) // TODO: move to redis?

	clientStore := store.NewClientStore()
	clientStore.Set("client", &models.Client{
		ID:     "client",
		Secret: "secret",                // replace this with actual secret
		Domain: "http://localhost:3000", // replace with environment domain
	})
	manager.MapClientStorage(clientStore)

	srv := server.NewDefaultServer(manager)
	srv.SetAllowGetAccessRequest(true)
	srv.SetClientInfoHandler(server.ClientFormHandler)

	mux := http.NewServeMux()
	a := &App{
		redisClient: client,
		scheduler:   scheduler,
		manager:     manager,
		srv:         srv,
		httpServer:  &http.Server{Addr: ":3000", Handler: mux},
	}

	// auth routes
	mux.HandleFunc("/auth/register", a.register)
	mux.HandleFunc("/auth/login", a.login)

	mux.HandleFunc("/oauth/authorize", a.authorize)
	mux.HandleFunc("/oauth/token", a.token)

	mux.HandleFunc("/jobs", a.enqueueJob)
	mux.HandleFunc("/jobs/status", a.getJobStatus)

	srv.UserAuthorizationHandler = a.UserAuthorizationHandler

	return a
}

func (a *App) Start() error {
	// Connect to redis
	if err := a.redisClient.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	// Launch HTTP server
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		log.Println("HTTP server listening on", a.httpServer.Addr)
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	if err := a.httpServer.Shutdown(ctx); err != nil {
		log.Printf("error shutting down HTTP server: %v", err)
	}

	// Wait for ListenAndServe goroutine to finish
	a.wg.Wait()

	if err := a.scheduler.Close(); err != nil {
		log.Printf("error closing scheduler: %v", err)
	}

	if err := a.redisClient.Close(); err != nil {
		log.Printf("error closing redis client: %v", err)
	}

	return nil
}

func main() {
	app := NewApp("localhost:6379", "AMD")

	if err := app.Start(); err != nil {
		log.Fatalf("failed to start app: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Println("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}

	log.Println("all services stopped cleanly")
}

func (a *App) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		a.jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.jsonResponse(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "Invalid JSON",
		})
		return
	}

	if req.Email == "" || req.Password == "" {
		a.jsonResponse(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "Email and password required",
		})
		return
	}

	// Check if user exists
	if _, err := a.getUserByEmail(req.Email); err == nil {
		a.jsonResponse(w, http.StatusConflict, APIResponse{
			Success: false,
			Error:   "Email already exists",
		})
		return
	}

	user, err := a.createUser(req.Email, req.Password)
	if err != nil {
		log.Printf("Failed to create user: %v", err)
		a.jsonResponse(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   "Failed to create user",
		})
		return
	}

	a.jsonResponse(w, http.StatusCreated, APIResponse{
		Success: true,
		Data: map[string]string{
			"user_id": user.ID,
			"email":   user.Email,
		},
	})
}

func (a *App) authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		sessionID := r.Header.Get("Authorization")
		if strings.HasPrefix(sessionID, "Session ") {
			sessionID = strings.TrimPrefix(sessionID, "Session ")
		}
		if cookie, err := r.Cookie("session"); err == nil {
			sessionID = cookie.Value
		}

		log.Println(sessionID)

		if sessionID == "" {
			log.Println("a")
			a.redirectToLogin(w, r)
			return
		}
		if _, err := a.getSession(sessionID); err != nil {
			log.Println("b")
			a.redirectToLogin(w, r)
			return
		}

		err := a.srv.HandleAuthorizeRequest(w, r)
		if err != nil {
			log.Printf("Authorize error: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (a *App) token(w http.ResponseWriter, r *http.Request) {
	err := a.srv.HandleTokenRequest(w, r)
	if err != nil {
		log.Printf("Token error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if sessionID := a.getSessionFromRequest(r); sessionID != "" {
			if _, err := a.getSession(sessionID); err == nil {
				// user is already logged in, redirect them
				redirectURL := r.URL.Query().Get("redirect")
				if redirectURL == "" {
					redirectURL = "/" // default
				}
				
				http.Redirect(w, r, redirectURL, http.StatusFound)
				return
			}
		}

		
		a.showLoginPage(w, r)
		return
	}

	if r.Method != "POST" {
		a.jsonResponse(w, http.StatusMethodNotAllowed, APIResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	// Check if this is a form submission or api
	contentType := r.Header.Get("Content-Type")
	isFormData := strings.Contains(contentType, "application/x-www-form-urlencoded") || contentType == ""

	var email, password string
	var err error

	if isFormData {
		email = r.FormValue("email")
		password = r.FormValue("password")
		if email == "" || password == "" {
			a.showLoginPage(w, r)
			return
		}
	} else {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.jsonResponse(w, http.StatusBadRequest, APIResponse{
				Success: false,
				Error:   "Invalid JSON",
			})
			return
		}
		email = req.Email
		password = req.Password
	}

	user, err := a.getUserByEmail(email)
	if err != nil {
		if isFormData {
			a.showLoginPage(w, r)
			return
		}
		a.jsonResponse(w, http.StatusUnauthorized, APIResponse{
			Success: false,
			Error:   "Invalid Email / Password",
		})
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		if isFormData {
			a.showLoginPage(w, r)
			return
		}
		a.jsonResponse(w, http.StatusUnauthorized, APIResponse{
			Success: false,
			Error:   "Invalid Email / Password",
		})
		return
	}

	sessionID, err := a.CreateSession(user.ID)
	if err != nil {
		if isFormData {
			a.showLoginPage(w, r)
			return
		}
		a.jsonResponse(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   "Error Creating Session",
		})
		return
	}

	if isFormData {
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Path:     "/",
			Value:    sessionID,
			HttpOnly: true,
			Secure:   false, // TODO: change in prod
			SameSite: http.SameSiteLaxMode,
		})

		redirectURL := r.FormValue("redirect")
		log.Println(redirectURL)
		if redirectURL == "" {
			redirectURL = "/" // Default redirect
		}
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	a.jsonResponse(w, http.StatusAccepted, APIResponse{
		Success: true,
		Data: map[string]any{
			"session": sessionID,
		},
	})
}

// TODO: move this to a file?
func (a *App) showLoginPage(w http.ResponseWriter, r *http.Request) {
	redirectURL := r.URL.Query().Get("redirect")

	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body>
    <form method="POST" action="/auth/login">
        <input type="hidden" name="redirect" value="%s">
        
        <div>
            <label>Email:</label>
            <input type="email" name="email" required>
        </div>
        <div>
            <label>Password:</label>
            <input type="password" name="password" required>
        </div>
        <button type="submit">Login</button>
    </form>
</body>
</html>`, redirectURL)
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}

func (a *App) enqueueJob(w http.ResponseWriter, r *http.Request) {
	payload := map[string]interface{}{
		"task_id": 123,
		"data":    "test_data_123",
	}
	if err := a.scheduler.Enqueue("jobType", payload); err != nil {
		http.Error(w, "enqueue failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, "enqueued")
}

func (a *App) getJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	fmt.Fprintln(w, "job id=", id)
}
