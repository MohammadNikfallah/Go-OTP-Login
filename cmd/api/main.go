package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/julienschmidt/httprouter"
)

type config struct {
	port int
}

type application struct {
	conf   config
	logger *log.Logger
}

func main() {
	conf := &config{port: 8000}

	logger := log.New(os.Stdout, "LOG\t", log.Ldate|log.Ltime)

	app := application{
		conf:   *conf,
		logger: logger,
	}

	router := httprouter.New()

	router.GET("/", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Welcome to My OTP Login project")
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", app.conf.port),
		Handler:      router,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	app.logger.Printf("Server starting on port: %d", app.conf.port)

	err := server.ListenAndServe()
	if err != nil {
		app.logger.Printf("Starting server failed: %s", err)
	}
}
