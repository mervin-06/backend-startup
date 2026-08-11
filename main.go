package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/rs/cors"
)

func isAllowedOrigin(origin string) bool {

	if origin == "" {
		return false
	}

	origin = strings.TrimSpace(origin)

	if origin == "" {
		return false
	}

	allowedOrigins := []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",

		// Frontend applications
		"https://startup-client.netlify.app",
		"https://startup-client-gilt.vercel.app",
	}

	for _, allowed := range allowedOrigins {

		if origin == allowed {
			return true
		}
	}

	// Render environment variable
	if envOrigin := strings.TrimSpace(
		os.Getenv("CLIENT_ORIGIN"),
	); envOrigin != "" {

		if origin == envOrigin {
			return true
		}
	}

	return false
}

func main() {

	// Load .env file (only affects local dev; Render uses its own env vars)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	mux := http.NewServeMux()

	// Routes
	mux.HandleFunc("/", Health)
	mux.HandleFunc("/application", Application)

	// CORS
	c := cors.New(cors.Options{

		AllowOriginFunc: isAllowedOrigin,

		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},

		AllowedHeaders: []string{
			"Content-Type",
			"Authorization",
		},

		AllowCredentials: true,
	})

	handler := c.Handler(mux)

	// Render provides PORT
	port := os.Getenv("PORT")

	if port == "" {
		port = "8000"
	}

	log.Printf("Server listening on :%s", port)

	err := http.ListenAndServe(
		":"+port,
		handler,
	)

	if err != nil {
		log.Fatal(err)
	}
}
