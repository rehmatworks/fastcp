//go:build !pam

package auth

// When the code is built without the "pam" build tag, use the shadow-based verifier
// as the default implementation for verifyPassword.
func verifyPassword(username, password string) bool {
	return verifyPasswordShadow(username, password)
}
