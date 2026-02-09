package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	burnTokenLen    = 32
	burnVerifierLen = sha256.Size
)

var errInvalidBurnToken = errors.New("invalid burn token")

type BurnToken [burnTokenLen]byte

func NewBurnToken() BurnToken {
	var token BurnToken
	fillRandom(token[:])
	return token
}

func ParseBurnToken(value string) (BurnToken, error) {
	var token BurnToken
	return token, token.UnmarshalText([]byte(value))
}

func (token BurnToken) String() string {
	return b64Encode(token[:])
}

func (token BurnToken) MarshalText() ([]byte, error) {
	return []byte(token.String()), nil
}

//goland:noinspection GoMixedReceiverTypes
func (token *BurnToken) UnmarshalText(text []byte) error {
	decoded, err := b64Decode(string(text))
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidBurnToken, err)
	}
	if len(decoded) != burnTokenLen {
		return fmt.Errorf("%w length: %d", errInvalidBurnToken, len(decoded))
	}
	copy(token[:], decoded)
	return nil
}

func burnTokenVerifier(rootSecret []byte, burnToken BurnToken) ([]byte, error) {
	key, err := deriveSubkey(rootSecret, "burn verifier v1")
	if err != nil {
		return nil, fmt.Errorf("error deriving burn verifier key: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(burnToken[:])
	return mac.Sum(nil), nil
}
