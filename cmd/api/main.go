package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/julienschmidt/httprouter"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

type database struct {
	dsn          string
	maxOpenConns int
	maxIdleConns int
	maxIdleTime  time.Duration
}

type redisConf struct {
	addr     string
	password string
	db       int
}

type config struct {
	port  int
	db    database
	redis redisConf
}

type application struct {
	conf   config
	logger *log.Logger
	cashe  *redis.Client
}

func main() {
	conf := &config{
		port: 8000,
		db: database{
			dsn:          "host=localhost port=5432 user=postgres password=1234 dbname=optlogin sslmode=disable",
			maxOpenConns: 25,
			maxIdleConns: 25,
			maxIdleTime:  time.Minute,
		},
		redis: redisConf{
			addr:     "localhost:6379",
			password: "secret",
			db:       0,
		},
	}

	logger := log.New(os.Stdout, "LOG\t", log.Ldate|log.Ltime)

	db, err := connectDB(conf.db)

	if err != nil {
		logger.Fatalf("Connecting to database failed: %s", err)
	}
	logger.Printf("successfully Conected to database")
	defer db.Close()

	redisClient, err := connectRedis(conf.redis)
	if err != nil {
		logger.Fatalf("Connecting to reddis server failed: %s", err)
	}
	logger.Printf("successfully connected to redis server")

	defer redisClient.Close()

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

	err = server.ListenAndServe()
	if err != nil {
		app.logger.Fatalf("Starting server failed: %s", err)
	}
}

func connectDB(conf database) (*sql.DB, error) {
	db, err := sql.Open("postgres", conf.dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(conf.maxOpenConns)
	db.SetMaxIdleConns(conf.maxIdleConns)
	db.SetConnMaxIdleTime(conf.maxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = db.PingContext(ctx)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func connectRedis(conf redisConf) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     conf.addr,
		Password: conf.password,
		DB:       conf.db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}

	return client, nil
}
