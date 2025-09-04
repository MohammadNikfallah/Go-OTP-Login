package main

import (
	"Go-OTP-Login/internal/data"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

type envelope map[string]interface{}

// send JSON error response
func (app *application) errorResponse(w http.ResponseWriter, status int, message interface{}) {
	env := envelope{"error": message}
	if err := app.writeJSON(w, status, env, nil); err != nil {
		app.logger.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// write JSON with optional headers
func (app *application) writeJSON(w http.ResponseWriter, status int, body envelope, headers http.Header) error {
	js, err := json.Marshal(body)
	if err != nil {
		return err
	}

	for k, v := range headers {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(js, '\n'))

	return nil
}

// parse and validate a single JSON object
func (app *application) readJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	const maxBytes = 104856
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var (
			syntaxErr  *json.SyntaxError
			typeErr    *json.UnmarshalTypeError
			invalidErr *json.InvalidUnmarshalError
		)

		switch {
		case errors.As(err, &syntaxErr):
			return fmt.Errorf("body contains badly-formed JSON (at character %d)", syntaxErr.Offset)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("body contains badly-formed JSON")
		case errors.As(err, &typeErr):
			if typeErr.Field != "" {
				return fmt.Errorf("body contains incorrect JSON type for field %q", typeErr.Field)
			}
			return fmt.Errorf("body contains incorrect JSON type (at character %d)", typeErr.Offset)
		case errors.Is(err, io.EOF):
			return errors.New("body must not be empty")
		case errors.As(err, &invalidErr):
			panic(err)
		default:
			return err
		}
	}

	// ensure a single JSON value
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("body must only contain a single JSON value")
	}

	return nil
}

// generate 4-digit OTP
func generateOTP() string {
	otp := make([]byte, 2)
	if _, err := rand.Read(otp); err != nil {
		log.Fatal("Error generating OTP:", err)
	}
	return fmt.Sprintf("%04d", int(otp[0])%10000)
}

// store OTP with TTL in Redis
func (app *application) storeOTPInRedis(ctx context.Context, phoneNumber, otp string) error {
	userData := map[string]string{"otp": otp}
	if err := app.cache.HSet(ctx, phoneNumber, userData).Err(); err != nil {
		return fmt.Errorf("failed to store user data in Redis: %w", err)
	}
	if err := app.cache.Expire(ctx, phoneNumber, 2*time.Minute).Err(); err != nil {
		return fmt.Errorf("failed to set expiration for Redis key: %w", err)
	}
	return nil
}

// verify OTP from Redis
func (app *application) verifyOTPInRedis(ctx context.Context, phoneNumber, otp string) error {
	data, err := app.cache.HGetAll(ctx, phoneNumber).Result()
	if err != nil {
		return fmt.Errorf("invalid or expired OTP")
	}
	if data["otp"] != otp {
		return fmt.Errorf("invalid OTP")
	}
	return nil
}

// create user if not exists
func (app *application) createUserIfNotExists(phoneNumber string) (*data.User, error) {
	user, err := app.models.User.GetByPhoneNumber(phoneNumber)
	if err == nil && user != nil {
		return user, nil
	}
	newUser := data.User{PhoneNumber: phoneNumber}
	if err := app.models.User.Insert(&newUser); err != nil {
		return nil, fmt.Errorf("failed to create a user: %s", err)
	}
	return &newUser, nil
}

// create JWT (HS256)
func (app *application) generateJWT(userID int64, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(app.jwtSecret)
}

const (
	otpRateLimitMax    = 3
	otpRateLimitWindow = 10 * time.Minute
)

var otpRateLimitScript = redis.NewScript(`
local key   = KEYS[1]
local win   = tonumber(ARGV[1]) -- window seconds

local exists = redis.call("EXISTS", key)
if exists == 0 then
  redis.call("SET", key, 1, "EX", win)
  return {1, win}
else
  local newCount = redis.call("INCR", key)
  local ttl = redis.call("TTL", key)
  return {newCount, ttl}
end
`)

// allowOTPRequest increments the counter and tells if it's allowed.
// It returns: allowed, count, remaining, resetAt.
func (app *application) allowOTPRequest(ctx context.Context, phone string) (bool, error) {
	key := "rl:otp:" + phone
	winSec := int64(otpRateLimitWindow / time.Second)

	res, err := otpRateLimitScript.Run(ctx, app.cache, []string{key}, winSec).Result()
	if err != nil {
		return false, err
	}

	arr, ok := res.([]interface{})
	if !ok || len(arr) != 2 {
		return false, fmt.Errorf("unexpected rate-limit result")
	}

	count := arr[0].(int64)

	allowed := count <= otpRateLimitMax

	return allowed, nil
}
