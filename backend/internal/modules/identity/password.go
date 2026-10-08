package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// PasswordParameters controls the resources used by Argon2id.
type PasswordParameters struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultPasswordParameters returns the Phase 1 deployment baseline.
func DefaultPasswordParameters() PasswordParameters {
	return PasswordParameters{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

// HashPassword returns a self-describing Argon2id password hash.
func HashPassword(password string, parameters PasswordParameters) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || utf8.RuneCountInString(password) > 1024 {
		return "", fmt.Errorf("%w: must contain 12 to 1024 characters", ErrInvalidPassword)
	}
	salt := make([]byte, parameters.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, parameters.Iterations, parameters.Memory, parameters.Parallelism, parameters.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, parameters.Memory, parameters.Iterations, parameters.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword checks a password and reports whether the hash needs upgrading.
func VerifyPassword(password, encoded string, current PasswordParameters) (bool, bool, error) {
	parameters, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, false, err
	}
	actual := argon2.IDKey([]byte(password), salt, parameters.Iterations, parameters.Memory, parameters.Parallelism, uint32(len(expected)))
	valid := subtle.ConstantTimeCompare(actual, expected) == 1
	needsRehash := valid && parameters != current
	return valid, needsRehash, nil
}

func parsePasswordHash(encoded string) (PasswordParameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return PasswordParameters{}, nil, nil, fmt.Errorf("invalid password hash format")
	}
	var parameters PasswordParameters
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &parameters.Memory, &parameters.Iterations, &parameters.Parallelism); err != nil {
		return PasswordParameters{}, nil, nil, fmt.Errorf("parse password parameters: %w", err)
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return PasswordParameters{}, nil, nil, fmt.Errorf("decode password salt: %w", err)
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return PasswordParameters{}, nil, nil, fmt.Errorf("decode password hash: %w", err)
	}
	parameters.SaltLength, parameters.KeyLength = uint32(len(salt)), uint32(len(hash))
	if parameters.Memory == 0 || parameters.Iterations == 0 || parameters.Parallelism == 0 || len(salt) < 16 || len(hash) < 16 {
		return PasswordParameters{}, nil, nil, fmt.Errorf("invalid password parameters")
	}
	return parameters, salt, hash, nil
}
