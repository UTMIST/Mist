package main

import (
	"fmt"
	"net/http"
)

func login(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!")
}

func refresh(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!")
}

func enqueueJob(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!")
}

func getJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	fmt.Println(id)
	fmt.Fprintf(w, "Hello, world!")
}

func main() {
	router := http.NewServeMux()

	router.HandleFunc("POST /auth/login", login)
	router.HandleFunc("POST /auth/refresh", refresh)
	router.HandleFunc("POST /jobs", enqueueJob)
	router.HandleFunc("GET /jobs/{id}/status", getJobStatus)

	server := http.Server{
		Addr:    ":3000",
		Handler: router,
	}

	server.ListenAndServe()
}
