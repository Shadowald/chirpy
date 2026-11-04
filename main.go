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

	"github.com/Shadowald/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
}

type User struct {
	Id        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
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
	newUser, err := cfg.dbQueries.CreateUser(req.Context(), user.Email)
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
	validatedChirp, statusCode, errMessage := validateChirp(req)
	if statusCode != 0 {
		respondWithError(w, statusCode, errMessage)
		return
	}

	newChirp, err := cfg.dbQueries.CreateChirp(req.Context(), database.CreateChirpParams{
		Body:   validatedChirp.Body,
		UserID: validatedChirp.UserId,
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

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	dbQueries := database.New(db)
	platform := os.Getenv("PLATFORM")
	apiCfg := &apiConfig{dbQueries: dbQueries, platform: platform}
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
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	server.ListenAndServe()
}
