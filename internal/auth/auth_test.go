package auth

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHashPasswordAndCheck(t *testing.T) {
	password := "mysecretpassword"
	hashedPassword, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Error hashing password: %v", err)
	}

	match, err := CheckPasswordHash(password, hashedPassword)
	if err != nil {
		t.Fatalf("Error checking password hash: %v", err)
	}

	if !match {
		t.Fatalf("Expected password to match hash, but it did not")
	}
}

func TestHashPasswordAndCheckWithWrongPassword(t *testing.T) {
	password := "mysecretpassword"
	wrongPassword := "wrongpassword"
	hashedPassword, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Error hashing password: %v", err)
	}

	match, err := CheckPasswordHash(wrongPassword, hashedPassword)
	if err != nil {
		t.Fatalf("Error checking password hash: %v", err)
	}

	if match {
		t.Fatalf("Expected password not to match hash, but it did")
	}
}

func TestHashPasswordAndCheckWithEmptyPassword(t *testing.T) {
	password := ""
	hashedPassword, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Error hashing password: %v", err)
	}

	match, err := CheckPasswordHash(password, hashedPassword)
	if err != nil {
		t.Fatalf("Error checking password hash: %v", err)
	}

	if !match {
		t.Fatalf("Expected empty password to match hash, but it did not")
	}
}

func TestHashPasswordAndCheckWithEmptyHash(t *testing.T) {
	password := "mysecretpassword"
	emptyHash := ""
	match, err := CheckPasswordHash(password, emptyHash)
	if err == nil {
		t.Fatalf("Expected error when checking password against empty hash, but got none")
	}

	if match {
		t.Fatalf("Expected password not to match empty hash, but it did")
	}
}

func TestHashPasswordAndCheckWithInvalidHash(t *testing.T) {
	password := "mysecretpassword"
	invalidHash := "invalidhash"
	match, err := CheckPasswordHash(password, invalidHash)
	if err == nil {
		t.Fatalf("Expected error when checking password against invalid hash, but got none")
	}

	if match {
		t.Fatalf("Expected password not to match invalid hash, but it did")
	}
}

func TestValidateJWTWithInvalidToken(t *testing.T) {
	tokenSecret := "mysecret"
	invalidToken := "invalidtoken"
	_, err := ValidateJWT(invalidToken, tokenSecret)
	if err == nil {
		t.Fatalf("Expected error when validating invalid JWT, but got none")
	}
}

func TestValidateJWTWithExpiredToken(t *testing.T) {
	tokenSecret := "mysecret"
	userID := uuid.New()
	// Create a token that expires immediately
	token, err := MakeJWT(userID, tokenSecret, -1*time.Second)
	if err != nil {
		t.Fatalf("Error creating JWT: %v", err)
	}

	_, err = ValidateJWT(token, tokenSecret)
	if err == nil {
		t.Fatalf("Expected error when validating expired JWT, but got none")
	}
}

func TestValidateJWTWithValidToken(t *testing.T) {
	tokenSecret := "mysecret"
	userID := uuid.New()
	// Create a token that expires in 1 hour
	token, err := MakeJWT(userID, tokenSecret, time.Hour)
	if err != nil {
		t.Fatalf("Error creating JWT: %v", err)
	}

	returnedUserID, err := ValidateJWT(token, tokenSecret)
	if err != nil {
		t.Fatalf("Error validating JWT: %v", err)
	}

	if returnedUserID != userID {
		t.Fatalf("Expected user ID %v, but got %v", userID, returnedUserID)
	}
}

func TestGetBearerToken(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer mytoken")
	token, err := GetBearerToken(headers)
	if err != nil {
		t.Fatalf("Error getting bearer token: %v", err)
	}

	if token != "mytoken" {
		t.Fatalf("Expected token 'mytoken', but got '%s'", token)
	}
}

func TestGetBearerTokenWithMissingHeader(t *testing.T) {
	headers := make(http.Header)
	_, err := GetBearerToken(headers)
	if err == nil {
		t.Fatalf("Expected error when getting bearer token with missing header, but got none")
	}
}

func TestGetBearerTokenWithInvalidFormat(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Authorization", "InvalidFormat mytoken")
	_, err := GetBearerToken(headers)
	if err == nil {
		t.Fatalf("Expected error when getting bearer token with invalid format, but got none")
	}
}
