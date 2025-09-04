package main

import (
	"Go-OTP-Login/internal/data"
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- Swagger helper models ---

// swagger:model requestOTPReq
type requestOTPReq struct {
	// required: true
	PhoneNumber string `json:"phone_number"`
}

// swagger:model verifyOTPReq
type verifyOTPReq struct {
	// required: true
	PhoneNumber string `json:"phone_number"`
	// required: true
	OTP string `json:"otp"`
}

// swagger:model verifyOTPRes
type verifyOTPRes struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Data    data.User `json:"data"`
	Token   string    `json:"token"` // JWT
}

// swagger:model protectedRes
type protectedRes struct {
	Message   string    `json:"message"`
	Phone     string    `json:"phone"`
	ExpiresAt time.Time `json:"expires_at"`
}

// --- OTP helpers (non-routes) ---

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

// --- HTTP handlers ---

// handleRequestOTP godoc
// @Summary     Request OTP
// @Description Generates OTP and stores it in Redis for the given phone_number (2 min TTL).
// @Tags        Auth
// @Accept      json
// @Produce     json
// @Param       payload body     requestOTPReq true "OTP request payload"
// @Success     200     {object} map[string]interface{} "success/message"
// @Failure     400     {object} map[string]string     "error"
// @Failure     500     {object} map[string]string     "error"
// @Router      /request [post]
func (app *application) handleRequestOTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PhoneNumber string `json:"phone_number"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.errorResponse(w, http.StatusBadRequest, "Invalid request payload")
		app.logger.Println("Error reading JSON:", err)
		return
	}
	if input.PhoneNumber == "" {
		app.errorResponse(w, http.StatusBadRequest, "Phone number is required")
		return
	}

	otp := generateOTP()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := app.storeOTPInRedis(ctx, input.PhoneNumber, otp); err != nil {
		app.errorResponse(w, http.StatusInternalServerError, "Failed to store OTP")
		app.logger.Println("Error storing OTP in Redis:", err)
		return
	}

	// NOTE: logging OTP is fine in dev; remove in prod
	app.logger.Printf("OTP for %s: %s\n", input.PhoneNumber, otp)

	_ = app.writeJSON(w, http.StatusOK, envelope{
		"success": true,
		"message": "OTP sent successfully",
	}, nil)
}

// handleVerifyOTP godoc
// @Summary     Verify OTP
// @Description Verifies OTP, creates user if needed, and returns a JWT.
// @Tags        Auth
// @Accept      json
// @Produce     json
// @Param       payload body     verifyOTPReq true "OTP verification payload"
// @Success     200     {object} verifyOTPRes
// @Failure     400     {object} map[string]string
// @Failure     401     {object} map[string]string
// @Failure     500     {object} map[string]string
// @Router      /verify [post]
func (app *application) handleVerifyOTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PhoneNumber string `json:"phone_number"`
		OTP         string `json:"otp"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.errorResponse(w, http.StatusBadRequest, "Invalid request payload")
		app.logger.Println("Error reading JSON:", err)
		return
	}
	if input.PhoneNumber == "" || input.OTP == "" {
		app.errorResponse(w, http.StatusBadRequest, "Phone number and OTP are required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := app.verifyOTPInRedis(ctx, input.PhoneNumber, input.OTP); err != nil {
		app.errorResponse(w, http.StatusUnauthorized, "Invalid or expired OTP")
		app.logger.Println("OTP verification failed for", input.PhoneNumber, ":", err)
		return
	}

	user, err := app.createUserIfNotExists(input.PhoneNumber)
	if err != nil {
		app.errorResponse(w, http.StatusInternalServerError, "Failed to register user")
		app.logger.Println("Error registering user:", err)
		return
	}

	jwtToken, err := app.generateJWT(user.ID, 48*time.Hour)
	if err != nil {
		app.errorResponse(w, http.StatusInternalServerError, "Failed to generate JWT")
		app.logger.Println("Error generating JWT for user ID", user.ID, ":", err)
		return
	}

	_ = app.writeJSON(w, http.StatusOK, envelope{
		"success": true,
		"message": "User authenticated",
		"data":    user,
		"token":   jwtToken,
	}, nil)
}

// protectedHandler godoc
// @Summary     Protected resource
// @Description Requires Bearer token (Authorization: Bearer <token>)
// @Tags        Protected
// @Security    BearerAuth
// @Produce     json
// @Success     200 {object} protectedRes
// @Failure     401 {object} map[string]string
// @Router      /protected [get]
func (app *application) protectedHandler(w http.ResponseWriter, r *http.Request) {
	user := app.contextGetUser(r)

	// Parse JWT again to read exp
	authHeader := r.Header.Get("Authorization")
	tokenStr := strings.Split(authHeader, " ")[1]

	parsed, _, err := new(jwt.Parser).ParseUnverified(tokenStr, &jwt.RegisteredClaims{})
	if err != nil {
		app.errorResponse(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || claims.ExpiresAt == nil {
		app.errorResponse(w, http.StatusUnauthorized, "Token missing expiration")
		return
	}

	resp := protectedRes{
		Message:   fmt.Sprintf("Hello %s!", user.PhoneNumber),
		Phone:     user.PhoneNumber,
		ExpiresAt: claims.ExpiresAt.Time,
	}

	_ = app.writeJSON(w, http.StatusOK, envelope{
		"message":    resp.Message,
		"phone":      resp.Phone,
		"expires_at": resp.ExpiresAt,
	}, nil)
}
