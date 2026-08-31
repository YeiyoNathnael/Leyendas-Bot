package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"leyendas-bot/internal/calendar"
)

func main() {
	_ = godotenv.Load()

	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = "http://localhost:8085/oauth/callback"
	}

	if clientID == "" || clientSecret == "" {
		fmt.Println("❌ Error: GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set in .env or environment.")
		fmt.Println("\nSetup Guide:")
		fmt.Println("1. Go to Google Cloud Console (https://console.cloud.google.com)")
		fmt.Println("2. Enable the Google Calendar API")
		fmt.Println("3. Create OAuth 2.0 Client ID (Web Application)")
		fmt.Println("4. Add Authorized redirect URI: " + redirectURL)
		fmt.Println("5. Set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET in .env")
		os.Exit(1)
	}

	oauthCfg := calendar.NewOAuthConfig(clientID, clientSecret, redirectURL)

	state := fmt.Sprintf("state-%d", time.Now().UnixNano())
	authURL := oauthCfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	fmt.Println("==================================================")
	fmt.Println("🔑 Google Calendar OAuth2 Authorization Generator")
	fmt.Println("==================================================")
	fmt.Println()
	fmt.Println("Please open the following URL in your browser:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	fmt.Println("Waiting for authorization callback on " + redirectURL + " ...")

	codeChan := make(chan string)
	server := &http.Server{Addr: ":8085"}

	http.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing code parameter", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintln(w, "<h2>Authorization Successful!</h2><p>You can close this tab and return to the terminal.</p>")
		codeChan <- code
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	var authCode string
	select {
	case authCode = <-codeChan:
		_ = server.Shutdown(context.Background())
	case <-time.After(5 * time.Minute):
		_ = server.Shutdown(context.Background())
		log.Fatal("Timed out waiting for authorization callback.")
	}

	ctx := context.Background()
	tok, err := oauthCfg.Exchange(ctx, authCode)
	if err != nil {
		log.Fatalf("Failed to exchange authorization code for token: %v", err)
	}

	fmt.Println()
	fmt.Println("==================================================")
	fmt.Println("✅ SUCCESS! Refresh Token Generated")
	fmt.Println("==================================================")
	fmt.Println()
	fmt.Println("Add this line to your .env file:")
	fmt.Println()
	fmt.Printf("GOOGLE_REFRESH_TOKEN=%s\n", tok.RefreshToken)
	fmt.Println()
	fmt.Println("==================================================")
}
