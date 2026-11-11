package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Shadowald/chirpy/internal/auth"
	"github.com/Shadowald/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
	jwtSecret      string
}

type User struct {
	Id           uuid.UUID `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Password     string    `json:"password"`
	Email        string    `json:"email"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
}

type Chirp struct {
	Id        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserId    uuid.UUID `json:"user_id"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, req)
	})
}

func (cfg *apiConfig) getFileserverHits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	template := `<html>
  		<body>
    		<h1>Welcome, Chirpy Admin</h1>
    		<p>Chirpy has been visited %d times!</p>
  		</body>
	</html>`
	w.Write([]byte(fmt.Sprintf(template, cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) resetMetrics(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		respondWithError(w, http.StatusForbidden, "Resetting metrics is only allowed in dev environment")
		return
	}
	databaseErr := cfg.dbQueries.ResetUsers(r.Context())
	if databaseErr != nil {
		log.Printf("Error resetting users: %v", databaseErr)
		respondWithError(w, http.StatusInternalServerError, "Error resetting users")
		return
	}
	cfg.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Metrics reset"))
}

func respondWithError(w http.ResponseWriter, statusCode int, message string) {
	type returnVals struct {
		Err string `json:"error"`
	}
	respBody := returnVals{
		Err: message,
	}
	data, err := json.Marshal(respBody)
	if err != nil {
		log.Printf("Error marshaling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(data)
}

func respondWithJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshaling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(data)
}

func censorChirp(chirp string) string {
	censoredWords := []string{"kerfuffle", "sharbert", "fornax"}
	words := strings.Split(chirp, " ")
	for _, word := range censoredWords {
		for i, w := range words {
			w = strings.ToLower(w)
			if w == word {
				words[i] = "****"
			}
		}
	}
	cleaned_chirp := strings.Join(words, " ")
	return cleaned_chirp
}

func validateChirp(req *http.Request) (Chirp, int, string) {
	decoder := json.NewDecoder(req.Body)
	chirp := Chirp{}
	err := decoder.Decode(&chirp)
	if err != nil {
		return Chirp{}, http.StatusBadRequest, "Invalid JSON"
	}
	if len(chirp.Body) > 140 {
		return Chirp{}, http.StatusBadRequest, "Chirp is too long"
	}
	chirp.Body = censorChirp(chirp.Body)
	return chirp, 0, ""
}

func (cfg *apiConfig) addUser(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	user := User{}
	err := decoder.Decode(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if user.Email == "" {
		respondWithError(w, http.StatusBadRequest, "Email is required")
		return
	}

	hashedPassword, err := auth.HashPassword(user.Password)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error hashing password")
		return
	}

	newUser, err := cfg.dbQueries.CreateUser(req.Context(), database.CreateUserParams{
		HashedPassword: hashedPassword,
		Email:          user.Email,
	})
	if err != nil {
		log.Printf("Error creating user: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating user")
		return
	}

	user = User{
		Id:        newUser.ID,
		CreatedAt: newUser.CreatedAt,
		UpdatedAt: newUser.UpdatedAt,
		Email:     newUser.Email,
	}
	respondWithJSON(w, http.StatusCreated, user)
}

func (cfg *apiConfig) postChirp(w http.ResponseWriter, req *http.Request) {
	bearerToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := auth.ValidateJWT(bearerToken, cfg.jwtSecret)
	if err != nil {
		log.Printf("Error validating JWT: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	validatedChirp, statusCode, errMessage := validateChirp(req)
	if statusCode != 0 {
		respondWithError(w, statusCode, errMessage)
		return
	}

	newChirp, err := cfg.dbQueries.CreateChirp(req.Context(), database.CreateChirpParams{
		Body:   validatedChirp.Body,
		UserID: userID,
	})
	if err != nil {
		log.Printf("Error creating chirp: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating chirp")
		return
	}
	chirp := Chirp{
		Id:        newChirp.ID,
		CreatedAt: newChirp.CreatedAt,
		UpdatedAt: newChirp.UpdatedAt,
		Body:      newChirp.Body,
		UserId:    newChirp.UserID,
	}

	respondWithJSON(w, http.StatusCreated, chirp)
}

func (cfg *apiConfig) retrieveChirps(w http.ResponseWriter, req *http.Request) {
	retrievedChirps, err := cfg.dbQueries.RetrieveChirps(req.Context())
	if err != nil {
		log.Printf("Error retrieving chirps: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error retrieving chirps")
		return
	}

	chirps := make([]Chirp, len(retrievedChirps))
	for i := range retrievedChirps {
		chirps[i] = Chirp{
			Id:        retrievedChirps[i].ID,
			CreatedAt: retrievedChirps[i].CreatedAt,
			UpdatedAt: retrievedChirps[i].UpdatedAt,
			Body:      retrievedChirps[i].Body,
			UserId:    retrievedChirps[i].UserID,
		}
	}

	respondWithJSON(w, http.StatusOK, chirps)
}

func (cfg *apiConfig) getChirpById(w http.ResponseWriter, req *http.Request) {
	// Extract the chirp ID from the URL path
	chirpID := strings.TrimPrefix(req.URL.Path, "/api/chirps/")
	if chirpID == "" {
		respondWithError(w, http.StatusBadRequest, "Chirp ID is required")
		return
	}
	chirpUUID, err := uuid.Parse(chirpID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Chirp ID format")
		return
	}

	// Retrieve the chirp from the database
	retrievedChirp, err := cfg.dbQueries.RetrieveChirpByID(req.Context(), chirpUUID)
	if err != nil {
		log.Printf("Error retrieving chirp: %v", err)
		respondWithError(w, http.StatusNotFound, "Chirp not found")
		return
	}

	chirp := Chirp{
		Id:        retrievedChirp.ID,
		CreatedAt: retrievedChirp.CreatedAt,
		UpdatedAt: retrievedChirp.UpdatedAt,
		Body:      retrievedChirp.Body,
		UserId:    retrievedChirp.UserID,
	}

	respondWithJSON(w, http.StatusOK, chirp)
}

func (cfg *apiConfig) userLogin(w http.ResponseWriter, req *http.Request) {
	decoder := json.NewDecoder(req.Body)
	type Body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	body := Body{}
	err := decoder.Decode(&body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	if body.Email == "" || body.Password == "" {
		respondWithError(w, http.StatusBadRequest, "Email and password are required")
		return
	}
	userFromDB, err := cfg.dbQueries.RetrieveUserByEmail(req.Context(), body.Email)
	if err != nil {
		log.Printf("Error retrieving user: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}
	match, err := auth.CheckPasswordHash(body.Password, userFromDB.HashedPassword)
	if err != nil || !match {
		log.Printf("Error checking password: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	user := User{
		Id:        userFromDB.ID,
		CreatedAt: userFromDB.CreatedAt,
		UpdatedAt: userFromDB.UpdatedAt,
		Email:     userFromDB.Email,
	}

	token, err := auth.MakeJWT(user.Id, cfg.jwtSecret, time.Duration(1)*time.Hour)
	if err != nil {
		log.Printf("Error creating JWT: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating JWT")
		return
	}
	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		log.Printf("Error creating refresh token: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating refresh token")
		return
	}

	_, err = cfg.dbQueries.SaveRefreshToken(req.Context(), database.SaveRefreshTokenParams{
		Token:     refreshToken,
		UserID:    user.Id,
		ExpiresAt: time.Now().Add(60 * 24 * time.Hour), // 60 days
	})
	if err != nil {
		log.Printf("Error saving refresh token: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error saving refresh token")
		return
	}

	user.Token = token
	user.RefreshToken = refreshToken
	respondWithJSON(w, http.StatusOK, user)
}

func (cfg *apiConfig) refreshToken(w http.ResponseWriter, req *http.Request) {
	bearerToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := cfg.dbQueries.GetRefreshToken(req.Context(), bearerToken)
	if err != nil {
		log.Printf("Error retrieving refresh token: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	if token.ExpiresAt.Before(time.Now()) {
		respondWithError(w, http.StatusUnauthorized, "Refresh token has expired")
		return
	}

	if token.RevokedAt.Valid {
		respondWithError(w, http.StatusUnauthorized, "Refresh token has been revoked")
		return
	}

	accessToken, err := auth.MakeJWT(token.UserID, cfg.jwtSecret, time.Duration(1)*time.Hour)
	if err != nil {
		log.Printf("Error creating JWT: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error creating JWT")
		return
	}

	response := map[string]string{
		"token": accessToken,
	}
	respondWithJSON(w, http.StatusOK, response)
}

func (cfg *apiConfig) revokeRefreshToken(w http.ResponseWriter, req *http.Request) {
	bearerToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := cfg.dbQueries.GetRefreshToken(req.Context(), bearerToken)
	if err != nil {
		log.Printf("Error retrieving refresh token: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	if token.RevokedAt.Valid {
		respondWithError(w, http.StatusUnauthorized, "Refresh token has already been revoked")
		return
	}

	err = cfg.dbQueries.UpdateRevokeRefreshToken(req.Context(), bearerToken)
	if err != nil {
		log.Printf("Error revoking refresh token: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error revoking refresh token")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) updateUser(w http.ResponseWriter, req *http.Request) {
	bearerToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	userID, err := auth.ValidateJWT(bearerToken, cfg.jwtSecret)
	if err != nil {
		log.Printf("Error validating JWT: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	decoder := json.NewDecoder(req.Body)
	user := User{}
	err = decoder.Decode(&user)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	hashedPassword, err := auth.HashPassword(user.Password)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error hashing password")
		return
	}

	err = cfg.dbQueries.UpdateUser(req.Context(), database.UpdateUserParams{
		ID:             userID,
		HashedPassword: hashedPassword,
		Email:          user.Email,
	})
	if err != nil {
		log.Printf("Error updating user: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error updating user")
		return
	}

	respondWithJSON(w, http.StatusOK, User{
		Id:    userID,
		Email: user.Email,
	})
}

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, req *http.Request) {
	bearerToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	userID, err := auth.ValidateJWT(bearerToken, cfg.jwtSecret)
	if err != nil {
		log.Printf("Error validating JWT: %v", err)
		respondWithError(w, http.StatusUnauthorized, "Invalid or expired token")
		return
	}

	chirpID := strings.TrimPrefix(req.URL.Path, "/api/chirps/")
	if chirpID == "" {
		respondWithError(w, http.StatusBadRequest, "Chirp ID is required")
		return
	}
	chirpUUID, err := uuid.Parse(chirpID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Chirp ID format")
		return
	}

	userChirp, err := cfg.dbQueries.RetrieveChirpByID(req.Context(), chirpUUID)
	if err != nil {
		log.Printf("Error retrieving chirp: %v", err)
		respondWithError(w, http.StatusNotFound, "Chirp not found")
		return
	}

	if userChirp.UserID != userID {
		respondWithError(w, http.StatusForbidden, "You are not authorized to delete this chirp")
		return
	}

	err = cfg.dbQueries.DeleteChirp(req.Context(), database.DeleteChirpParams{
		ID:     chirpUUID,
		UserID: userID,
	})
	if err != nil {
		log.Printf("Error deleting chirp: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Error deleting chirp")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	dbQueries := database.New(db)
	platform := os.Getenv("PLATFORM")
	jwtSecret := os.Getenv("JWT_SECRET")
	apiCfg := &apiConfig{dbQueries: dbQueries, platform: platform, jwtSecret: jwtSecret}
	mux := http.NewServeMux()
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app/", http.FileServer(http.Dir(".")))))
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /api/chirps", apiCfg.postChirp)
	mux.HandleFunc("GET /admin/metrics", apiCfg.getFileserverHits)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetMetrics)
	mux.HandleFunc("POST /api/users", apiCfg.addUser)
	mux.HandleFunc("GET /api/chirps", apiCfg.retrieveChirps)
	mux.HandleFunc("GET /api/chirps/", apiCfg.getChirpById)
	mux.HandleFunc("POST /api/login", apiCfg.userLogin)
	mux.HandleFunc("POST /api/refresh", apiCfg.refreshToken)
	mux.HandleFunc("POST /api/revoke", apiCfg.revokeRefreshToken)
	mux.HandleFunc("PUT /api/users", apiCfg.updateUser)
	mux.HandleFunc("DELETE /api/chirps/", apiCfg.deleteChirp)
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	server.ListenAndServe()
}
