package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// RegisterOIDCRoutes registers OIDC authentication routes
func (a *OIDCAuth) RegisterRoutes(router *gin.Engine) {
	router.GET("/auth/login", a.handleLogin)
	router.GET("/auth/callback", a.handleCallback)
	router.GET("/auth/logout", a.handleLogout)
}

// handleLogin initiates the OIDC login flow
func (a *OIDCAuth) handleLogin(c *gin.Context) {
	session := sessions.Default(c)

	state, err := GenerateState()
	if err != nil {
		slog.Error("failed to generate state", "error", err)
		c.String(http.StatusInternalServerError, "Internal error")
		return
	}

	session.Set(SessionKeyOIDCState, state)

	var authURL string
	if a.providerType == "github" {
		authURL = a.oauth2Config.AuthCodeURL(state)
	} else {
		nonce, err := GenerateNonce()
		if err != nil {
			slog.Error("failed to generate nonce", "error", err)
			c.String(http.StatusInternalServerError, "Internal error")
			return
		}
		session.Set(SessionKeyOIDCNonce, nonce)
		authURL = a.oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce))
	}

	if err := session.Save(); err != nil {
		slog.Error("failed to save session", "error", err)
		c.String(http.StatusInternalServerError, "Internal error")
		return
	}

	c.Redirect(http.StatusFound, authURL)
}

// handleCallback processes the OIDC callback
func (a *OIDCAuth) handleCallback(c *gin.Context) {
	session := sessions.Default(c)
	ctx := c.Request.Context()

	// Verify state
	expectedState := session.Get(SessionKeyOIDCState)
	if expectedState == nil || c.Query("state") != expectedState.(string) {
		c.String(http.StatusBadRequest, "Invalid state parameter")
		return
	}

	// Check for error from provider
	if errParam := c.Query("error"); errParam != "" {
		errDesc := c.Query("error_description")
		slog.Error("OIDC provider returned error", "error", errParam, "description", errDesc)
		c.String(http.StatusUnauthorized, "Authentication failed: %s", errDesc)
		return
	}

	// Exchange code for token
	code := c.Query("code")
	if code == "" {
		c.String(http.StatusBadRequest, "Missing authorization code")
		return
	}

	token, err := a.oauth2Config.Exchange(ctx, code)
	if err != nil {
		slog.Error("failed to exchange token", "error", err)
		c.String(http.StatusInternalServerError, "Failed to exchange token")
		return
	}

	var email string

	if a.providerType == "github" {
		// GitHub: Fetch user email from API
		email, err = a.fetchGitHubEmail(ctx, token.AccessToken)
		if err != nil {
			slog.Error("failed to fetch GitHub email", "error", err)
			c.String(http.StatusInternalServerError, "Failed to get user info")
			return
		}
	} else {
		// Standard OIDC: Extract from ID token
		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok {
			c.String(http.StatusInternalServerError, "Missing ID token")
			return
		}

		idToken, err := a.verifier.Verify(ctx, rawIDToken)
		if err != nil {
			slog.Error("failed to verify ID token", "error", err)
			c.String(http.StatusUnauthorized, "Invalid ID token")
			return
		}

		// Extract claims
		var claims struct {
			Nonce string `json:"nonce"`
			Email string `json:"email"`
		}
		if err := idToken.Claims(&claims); err != nil {
			slog.Error("failed to parse claims", "error", err)
			c.String(http.StatusInternalServerError, "Failed to parse claims")
			return
		}

		// Verify nonce
		expectedNonce := session.Get(SessionKeyOIDCNonce)
		if expectedNonce != nil && claims.Nonce != expectedNonce.(string) {
			c.String(http.StatusBadRequest, "Invalid nonce")
			return
		}

		email = claims.Email
	}

	if email == "" {
		c.String(http.StatusInternalServerError, "No email address found")
		return
	}

	// Check authorization
	if !a.IsUserAllowed(email) {
		slog.Warn("unauthorized OIDC login attempt", "email", email)
		c.String(http.StatusForbidden, "Access denied: your email is not authorized")
		return
	}

	// Create session
	session.Set(SessionKeyOIDCEmail, email)
	session.Delete(SessionKeyOIDCState)
	session.Delete(SessionKeyOIDCNonce)
	session.Options(sessions.Options{
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   c.Request.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	if err := session.Save(); err != nil {
		slog.Error("failed to save session", "error", err)
		c.String(http.StatusInternalServerError, "Failed to create session")
		return
	}

	slog.Info("OIDC login successful", "email", email, "provider", a.providerType)
	c.Redirect(http.StatusFound, "/")
}

// handleLogout clears the session
func (a *OIDCAuth) handleLogout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	if err := session.Save(); err != nil {
		slog.Error("failed to clear session", "error", err)
	}
	c.Redirect(http.StatusFound, "/auth/login")
}

// fetchGitHubEmail fetches the primary email from GitHub API
func (a *OIDCAuth) fetchGitHubEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal(body, &emails); err != nil {
		return "", err
	}

	// First, try to find primary verified email
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	// Fall back to any verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}

	return "", fmt.Errorf("no verified email found")
}
