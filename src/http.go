package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"syscall"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func writeJSON(w http.ResponseWriter, code int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func executorError(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	var invalid *submissionError
	if errors.As(err, &invalid) {
		code = http.StatusBadRequest
	}
	if apierrors.IsNotFound(err) {
		code = http.StatusNotFound
	}
	var denied *permissionError
	if errors.As(err, &denied) {
		code = http.StatusForbidden
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		code = 507
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
