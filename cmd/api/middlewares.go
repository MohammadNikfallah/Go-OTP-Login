package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"Go-OTP-Login/internal/data"

	"github.com/golang-jwt/jwt/v5"
)

// recoverPanic recovers from panics in handlers and returns 500.
func (app *application) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.Header().Set("Connection", "close")
				app.errorResponse(w, http.StatusInternalServerError, "Failed to recover")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// authenticate validates Bearer JWT and sets user in context.
func (app *application) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Authorization")

		auth := r.Header.Get("Authorization")
		if auth == "" {
			r = app.contextSetUser(r, data.AnonymousUser)
			next.ServeHTTP(w, r)
			return
		}

		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			app.errorResponse(w, http.StatusUnauthorized, "Invalid authorization header")
			return
		}

		tokenStr := parts[1]
		parsed, err := jwt.ParseWithClaims(tokenStr, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return app.jwtSecret, nil
		})
		if err != nil || !parsed.Valid {
			app.errorResponse(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
		if !ok || claims.Subject == "" {
			app.errorResponse(w, http.StatusUnauthorized, "Invalid token claims")
			return
		}

		userID, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil {
			app.errorResponse(w, http.StatusUnauthorized, "Invalid token subject")
			return
		}

		user, err := app.models.User.GetByID(userID)
		if err != nil {
			app.errorResponse(w, http.StatusUnauthorized, "User not found")
			return
		}

		r = app.contextSetUser(r, user)
		next.ServeHTTP(w, r)
	})
}

// requireAuthenticatedUser blocks requests from AnonymousUser.
func (app *application) requireAuthenticatedUser(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := app.contextGetUser(r)
		if user.IsAnonymous() {
			app.errorResponse(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
