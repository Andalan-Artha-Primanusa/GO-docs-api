package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const passwordIterations = 120000

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := derive(password, salt, passwordIterations)
	return fmt.Sprintf("sha256$%d$%s$%s", passwordIterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func CheckPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := derive(password, salt, iterations)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func derive(password string, salt []byte, iterations int) []byte {
	sum := sha256.Sum256(append(salt, []byte(password)...))
	out := sum[:]
	for range iterations {
		next := sha256.Sum256(out)
		out = next[:]
	}
	return out
}
