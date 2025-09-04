package main

import (
	"Go-OTP-Login/internal/data"
	"context"
	"crypto/rand"
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
	cache  *redis.Client
	models data.Models
}

func main() {
	conf := &config{
		port: 8000,
		db: database{
			dsn:          "host=localhost port=5433 user=postgres password=1234 dbname=optlogin sslmode=disable",
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
	logger.Printf("successfully Conected to database\n")
	defer db.Close()

	redisClient, err := connectRedis(conf.redis)
	if err != nil {
		logger.Fatalf("Connecting to reddis server failed: %s", err)
	}
	logger.Printf("successfully connected to redis server\n")

	defer redisClient.Close()

	app := application{
		conf:   *conf,
		logger: logger,
		cache:  redisClient,
		models: data.NewModels(db),
	}

	router := httprouter.New()
	router.HandlerFunc(http.MethodPost, "/signup", app.signupUserHandler)
	router.HandlerFunc(http.MethodPost, "/verify", app.verifyAndRegisterUserHandler)

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

	app.logger.Printf("Server starting on port: %d\n", app.conf.port)

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

func (app *application) signupUserHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		PhoneNumber string `json:"phone_number"`
	}

	err := app.readJSON(w, r, &input)
	if err != nil {
		app.errorResponse(w, http.StatusBadRequest, "Invalid Request Payload")
		app.logger.Printf("Reading Json Failed:%s\n", err)
		return
	}

	if input.Name == "" || input.PhoneNumber == "" {
		app.errorResponse(w, http.StatusBadRequest, "Name and phone number are required")
		return
	}

	user, err := app.models.User.GetByPhoneNumber(input.PhoneNumber)
	if err == nil && user != nil {
		app.errorResponse(w, http.StatusConflict, "User already exists with the given phone number")
		return
	}

	otp := generateOTP()

	userData := map[string]string{
		"name": input.Name,
		"otp":  otp,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = app.cache.HSet(ctx, input.PhoneNumber, userData).Err()
	if err != nil {
		app.errorResponse(w, http.StatusInternalServerError, "Failed to store user data")
		app.logger.Println("Error storing user data in Redis:", err)
		return
	}

	err = app.cache.Expire(ctx, input.PhoneNumber, 5*time.Minute).Err()
	if err != nil {
		app.errorResponse(w, http.StatusInternalServerError, "Failed to set expiration for user data")
		app.logger.Println("Error setting expiration for Redis key:", err)
		return
	}

	app.logger.Println("Generated OTP for", input.PhoneNumber, ":", otp)

	app.writeJSON(w, http.StatusOK, envelope{"success": true, "message": "OTP sent successfully"}, nil)
}

func (app *application) verifyAndRegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		OTP         string `json:"otp"`
		PhoneNumber string `json:"phone_number"`
	}

	err := app.readJSON(w, r, &input)
	if err != nil {
		app.errorResponse(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.OTP == "" || input.PhoneNumber == "" {
		app.errorResponse(w, http.StatusBadRequest, "OTP and phone number are required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	userData, err := app.cache.HGetAll(ctx, input.PhoneNumber).Result()
	if err != nil || len(userData) == 0 {
		app.errorResponse(w, http.StatusUnauthorized, "Invalid or expired OTP")
		return
	}

	storedOTP := userData["otp"]
	if input.OTP != storedOTP {
		app.errorResponse(w, http.StatusUnauthorized, "Invalid OTP")
		return
	}

	userName := userData["name"]

	user := data.User{
		Name:        userName,
		PhoneNumber: input.PhoneNumber,
	}

	err = app.models.User.Insert(&user)

	if err != nil {
		app.errorResponse(w, http.StatusInternalServerError, "Failed to register user")
		app.logger.Println("Error registering user:", err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"success": true,
		"data":    user,
		"message": "User registered successfully",
	}, nil)
}

func generateOTP() string {
	otp := make([]byte, 2)

	_, err := rand.Read(otp)
	if err != nil {
		log.Fatal("Error generating OTP:", err)
	}
	return fmt.Sprintf("%04d", int(otp[0])%10000)
}
