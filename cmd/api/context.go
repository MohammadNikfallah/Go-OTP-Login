package main

import (
	"context"
	"net/http"

	"Go-OTP-Login/internal/data"
)

// contextKey prevents collisions with other context keys.
type contextKey string

// key for storing *data.User in request context.
const userContextKey contextKey = "OTP.user"

// attach user to request context
func (app *application) contextSetUser(r *http.Request, user *data.User) *http.Request {
	ctx := context.WithValue(r.Context(), userContextKey, user)
	return r.WithContext(ctx)
}

// get user from request context (panics if missing)
func (app *application) contextGetUser(r *http.Request) *data.User {
	user, ok := r.Context().Value(userContextKey).(*data.User)
	if !ok {
		panic("missing user context")
	}
	return user
}
