package main

import (
	"log"

	"example.com/shop/internal/db"
	"example.com/shop/internal/server"
)

func main() {
	pool, err := db.Open()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	if err := server.New(pool).ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
