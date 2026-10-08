package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestPasswordUnicodeLengthAndResourceBounds(t *testing.T) {
	parameters := PasswordParameters{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	for _, password := range []string{strings.Repeat("界", 11), strings.Repeat("a", 1025), string([]byte{0xff}) + strings.Repeat("a", 12)} {
		if _, err := HashPassword(password, parameters); !errors.Is(err, ErrInvalidPassword) {
			t.Fatalf("invalid length/encoding accepted: %v", err)
		}
	}
	if _, err := HashPassword(strings.Repeat("界", 12), parameters); err != nil {
		t.Fatalf("valid Unicode password rejected: %v", err)
	}
}

func TestPasswordRoundTripAndRehash(t *testing.T) {
	parameters := DefaultPasswordParameters()
	encoded, err := HashPassword("correct horse battery staple", parameters)
	if err != nil {
		t.Fatal(err)
	}
	valid, rehash, err := VerifyPassword("correct horse battery staple", encoded, parameters)
	if err != nil || !valid || rehash {
		t.Fatalf("valid=%v rehash=%v err=%v", valid, rehash, err)
	}
	stronger := parameters
	stronger.Iterations++
	valid, rehash, err = VerifyPassword("correct horse battery staple", encoded, stronger)
	if err != nil || !valid || !rehash {
		t.Fatalf("valid=%v rehash=%v err=%v", valid, rehash, err)
	}
	valid, _, err = VerifyPassword("wrong password", encoded, parameters)
	if err != nil || valid {
		t.Fatalf("valid=%v err=%v", valid, err)
	}
}
