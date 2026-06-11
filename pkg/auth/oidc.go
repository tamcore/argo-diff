package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jwx-go/jwkfetch/v4"
	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

const (
	GitHubIssuer  = "https://token.actions.githubusercontent.com"
	GitHubJWKSURL = "https://token.actions.githubusercontent.com/.well-known/jwks"

	// jwksMinRefreshInterval is the minimum interval between JWKS refreshes
	jwksMinRefreshInterval = 15 * time.Minute
)

type OIDCValidator struct {
	jwksURL string
	cache   *jwkfetch.Cache
}

// NewOIDCValidator creates a validator with a background-refreshing JWKS cache.
// The provided context controls the lifetime of the cache refresh loop.
func NewOIDCValidator(ctx context.Context) (*OIDCValidator, error) {
	httprcClient := httprc.NewClient()
	cache, err := jwkfetch.NewCache(ctx, httprcClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS cache: %w", err)
	}

	if err := cache.Register(ctx, GitHubJWKSURL, jwkfetch.WithMinInterval(jwksMinRefreshInterval)); err != nil {
		return nil, fmt.Errorf("failed to register JWKS URL: %w", err)
	}

	return &OIDCValidator{
		jwksURL: GitHubJWKSURL,
		cache:   cache,
	}, nil
}

func (v *OIDCValidator) ValidateToken(ctx context.Context, tokenString string) (string, error) {
	keySet, err := v.cache.Fetch(ctx, v.jwksURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	token, err := jwt.Parse(
		[]byte(tokenString),
		jwt.WithKeySet(keySet),
		jwt.WithValidate(true),
		jwt.WithIssuer(GitHubIssuer),
	)
	if err != nil {
		return "", fmt.Errorf("failed to parse/validate token: %w", err)
	}

	repo, err := jwt.Get[string](token, "repository")
	if err != nil {
		return "", fmt.Errorf("token missing or invalid 'repository' claim: %w", err)
	}
	if repo == "" {
		return "", fmt.Errorf("invalid 'repository' claim format")
	}

	return repo, nil
}

func ExtractBearerToken(authHeader string) (string, error) {
	if authHeader == "" {
		return "", fmt.Errorf("missing Authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid Authorization header format")
	}

	if !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("authorization header must use Bearer scheme")
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}

	return token, nil
}
