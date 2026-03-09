package audible

import "errors"

// Common errors
var (
	ErrNotAuthenticated   = errors.New("client is not authenticated")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTokenExpired       = errors.New("access token has expired")
	ErrInvalidResponse    = errors.New("invalid API response")
	ErrNotFound           = errors.New("resource not found")
	ErrRateLimited        = errors.New("rate limited by API")
	ErrInvalidDataLength  = errors.New("data length must be a multiple of 4 bytes")
	ErrInvalidKeyLength   = errors.New("key must be 16 bytes")
	ErrDataTooShort       = errors.New("data must contain at least 2 uint32 values")
	ErrInvalidActivation  = errors.New("invalid activation blob")
)
