package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/user/catraca/internal/api"
	"github.com/user/catraca/internal/db"
	"github.com/user/catraca/internal/henry"
)

func main() {
	db.InitDB()

	api.HC = henry.NewHenryClient("", 3000)

	if os.Getenv("HENRY_SIMULATOR") == "1" {
		go henry.StartHenrySimulator()
		fmt.Println("Simulador Henry habilitado (HENRY_SIMULATOR=1)")
	}

	mux := http.NewServeMux()

	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/", fs)

	api.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         ":8082",
		Handler:      api.CorsMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		fmt.Println("\nEncerrando servidor...")
		if api.HC != nil {
			api.HC.Disconnect()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	fmt.Println("Servidor iniciado em http://localhost:8082")
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	fmt.Println("Servidor encerrado com sucesso.")
}
