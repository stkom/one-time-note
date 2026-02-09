package main

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

func b64Decode(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("empty base64url value")
	}
	if base64.RawURLEncoding.EncodedLen(base64.RawURLEncoding.DecodedLen(len(value))) != len(value) {
		return nil, errors.New("non-canonical base64url length")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if b64Encode(decoded) != value {
		return nil, errors.New("non-canonical base64url encoding")
	}
	return decoded, nil
}

func b64Encode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// closeAndLogError attempts to close the given io.Closer and logs if an error occurs.
func closeAndLogError(c io.Closer) {
	if err := c.Close(); err != nil {
		logServerError("resource_close_failed", err)
	}
}

func deriveSubkey(rootSecret []byte, label string) ([]byte, error) {
	return hkdf.Key(sha256.New, rootSecret, nil, label, 32)
}

func fillRandom(bytes []byte) {
	// crypto/rand.Read always fills bytes and never returns an error.
	// See https://pkg.go.dev/crypto/rand#Read.
	_, _ = rand.Read(bytes)
}
