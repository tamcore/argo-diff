package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	GitHubIssuer  = "https://token.actions.githubusercontent.com"
	GitHubJWKSURL = "https://token.actions.githubusercontent.com/.well-known/jwks"
)

type OIDCValidator struct {
	jwksURL string
}

func NewOIDCValidator() *OIDCValidator {
	return &OIDCValidator{
		jwksURL: GitHubJWKSURL,
	}
}

func (v *OIDCValidator) ValidateToken(ctx context.Context, tokenString string) (string, error) {
	keySet, err := jwk.Fetch(ctx, v.jwksURL)
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

	repoClaim, ok := token.Get("repository")
	if !ok {
		return "", fmt.Errorf("token missing 'repository' claim")
	}

	repo, ok := repoClaim.(string)
	if !ok || repo == "" {
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
