// Package passhash wraps bcrypt password hashing (specs/08.7: no plaintext secrets).
package passhash

import "golang.org/x/crypto/bcrypt"

func Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
