package app

import (
	"context"
	"net/http"
)

type contextKey string

const userIDKey contextKey = "userID"

func withUserID(r *http.Request, userID int64) *http.Request {
	ctx := context.WithValue(r.Context(), userIDKey, userID)
	return r.WithContext(ctx)
}

func currentUserID(r *http.Request) int64 {
	id, _ := r.Context().Value(userIDKey).(int64)
	return id
}
