package auth

import (
	"errors"
	"net/http"
	"strings"
)

func GetAPIKey(headers http.Header) (string, error) {
	authHeader := headers.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("authorization header is missing")
	}

	if !strings.HasPrefix(authHeader, "ApiKey ") {
		return "", errors.New("invalid authorization header format")
	}

	return strings.TrimPrefix(authHeader, "ApiKey "), nil
}
