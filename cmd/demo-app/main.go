package main

import (
	"fmt"
	"net/http"
	"os"
)

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Health check passed"))
}

func version(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Version 1.0.0"))
}

func work(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("FAIL_MODE") == "true" {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Worker failed"))
		return
	}
	os.Setenv("FAIL_MODE", "false")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Worker is running"))
}

func main() {
	http.HandleFunc("/health", health)
	http.HandleFunc("/version", version)
	http.HandleFunc("/work", work)
	fmt.Println("IncidentLens demo app")
	http.ListenAndServe(":8080", nil)
}
