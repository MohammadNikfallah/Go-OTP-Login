package main

import (
	"Go-OTP-Login/internal/data"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/julienschmidt/httprouter"
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	allowed, err := app.allowOTPRequest(ctx, input.PhoneNumber)
	if err != nil {
		app.errorResponse(w, http.StatusInternalServerError, "rate limit error")
		app.logger.Println("rate limit error:", err)
		return
	}
	if !allowed {
		app.errorResponse(w, http.StatusTooManyRequests, "Too many OTP requests. Please try again later.")
		return
	}

	otp := generateOTP()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
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

// SingleUserEnvelope is the response wrapper for a single user.
type SingleUserEnvelope struct {
	User data.User `json:"user"`
}

// getSingleUser godoc
// @Summary      Get user by ID
// @Description  Retrieve a single user by its numeric ID.
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      200  {object}  SingleUserEnvelope
// @Failure      400  {object}  map[string]string  "invalid user id"
// @Failure      404  {object}  map[string]string  "user not found"
// @Failure      500  {object}  map[string]string  "failed to fetch user"
// @Security     BearerAuth
// @Router       /users/{id} [get]
func (app *application) getSingleUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	idStr := ps.ByName("id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		app.errorResponse(w, http.StatusBadRequest, "invalid user id")
		return
	}

	user, err := app.models.User.GetByID(userID)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.errorResponse(w, http.StatusNotFound, "user not found")
			return
		}
		app.logger.Println("get user error:", err)
		app.errorResponse(w, http.StatusInternalServerError, "failed to fetch user")
		return
	}

	_ = app.writeJSON(w, http.StatusOK, envelope{"user": user}, nil)
}

// UsersListResponse is the payload returned for user listing.
type UsersListResponse struct {
	Items    []data.User `json:"items"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	Total    int         `json:"total"`
}

// UsersListResponseEnvelope is used only for Swagger to document the envelope shape.
type UsersListResponseEnvelope struct {
	Response UsersListResponse `json:"response"`
}

// handleListUsers returns a paginated list of users with optional search.
//
// @Summary      List users
// @Description  Paginated list of users. Supports search by phone or other fields via 'q'.
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        q          query     string  false  "Search term (matches phone)"
// @Param        page       query     int     false  "Page number (1-based, default 1)"
// @Param        page_size  query     int     false  "Page size (max 100, default 20)"
// @Success      200  {object}  map[string]UsersListResponseEnvelope  "envelope with 'response' key"
// @Failure      500  {object}  map[string]string  "failed to fetch users"
// @Security     BearerAuth
// @Router       /users [get]
func (app *application) handleListUsers(w http.ResponseWriter, r *http.Request) {
	qp := r.URL.Query()

	q := strings.TrimSpace(qp.Get("q"))

	page := atoiDefault(qp.Get("page"), 1)
	if page < 1 {
		page = 1
	}
	pageSize := atoiDefault(qp.Get("page_size"), 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	filter := data.UserFilter{
		Q:        q,
		Page:     page,
		PageSize: pageSize,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	users, total, err := app.models.User.List(ctx, filter)
	if err != nil {
		app.logger.Println("list users error:", err)
		app.errorResponse(w, http.StatusInternalServerError, "failed to fetch users")
		return
	}

	resp := UsersListResponse{
		Items:    users,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}
	app.writeJSON(w, http.StatusOK, envelope{
		"response": resp,
	}, nil)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
