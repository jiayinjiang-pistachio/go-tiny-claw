package main

import (
	"fmt"
	"log"
	"net/http"
)

func pingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "pong")
}

func main() {
	http.HandleFunc("/ping", pingHandler)
	
	port := ":8080"
	log.Printf("Server starting on port %s...", port)
	log.Printf("Access http://localhost%s/ping to test", port)
	
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal("Server error:", err)
	}
}