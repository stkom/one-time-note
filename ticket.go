package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

type TicketData struct {
	V            int       `json:"v"`
	ID           NoteID    `json:"id"`
	IssuedAt     time.Time `json:"iat"`
	Exp          time.Time `json:"exp"`
	BurnVerifier string    `json:"burnVerifier"`
}

type Ticket struct {
	Value     string
	Data      TicketData
	BurnToken BurnToken
}

func (s *NoteService) CreateTicket() (Ticket, error) {
	burnToken := NewBurnToken()
	now := s.now()

	var ticket Ticket
	err := s.db.View(func(tx *bolt.Tx) error {
		rootKey, err := getRootKey(tx.Bucket(rootKeysBucketName))
		if err != nil {
			return err
		}
		burnVerifier, err := burnTokenVerifier(rootKey.Secret, burnToken)
		if err != nil {
			return err
		}
		data := TicketData{
			V:            1,
			ID:           NewNoteID(),
			IssuedAt:     now,
			Exp:          now.Add(ticketTimeout),
			BurnVerifier: b64Encode(burnVerifier),
		}
		value, err := signTicket(rootKey.Secret, data)
		if err != nil {
			return err
		}
		ticket = Ticket{Value: value, Data: data, BurnToken: burnToken}
		return nil
	})
	if err != nil {
		return ticket, fmt.Errorf("error creating ticket: %w", err)
	}
	return ticket, nil
}

func (s *NoteService) verifyTicket(ticket string, pathID NoteID, burnToken BurnToken) (TicketData, []byte, error) {
	var data TicketData
	if len(ticket) > maxTicketLen {
		return data, nil, ErrTicketInvalid
	}
	if !strings.HasPrefix(ticket, "v1.") {
		return data, nil, ErrTicketInvalid
	}
	signedValue, signatureValue, ok := bytes.Cut([]byte(ticket), []byte("."))
	if !ok {
		return data, nil, ErrTicketInvalid
	}
	payloadValue, signatureValue, ok := bytes.Cut(signatureValue, []byte("."))
	if !ok || len(signatureValue) == 0 {
		return data, nil, ErrTicketInvalid
	}
	if string(signedValue) != "v1" {
		return data, nil, ErrTicketInvalid
	}

	payload, err := b64Decode(string(payloadValue))
	if err != nil {
		return data, nil, ErrTicketInvalid
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return data, nil, ErrTicketInvalid
	}
	burnVerifier, err := validateTicketPayload(data, pathID, s.now())
	if err != nil {
		return data, nil, err
	}

	var rootKey rootKeyRecord
	err = s.db.View(func(tx *bolt.Tx) error {
		var err error
		rootKey, err = getRootKey(tx.Bucket(rootKeysBucketName))
		return err
	})
	if err != nil {
		return data, nil, err
	}

	expectedSignature, err := ticketSignature(rootKey.Secret, []byte("v1."+string(payloadValue)))
	if err != nil {
		return data, nil, err
	}
	signature, err := b64Decode(string(signatureValue))
	if err != nil {
		return data, nil, ErrTicketInvalid
	}
	if !hmac.Equal(signature, expectedSignature) {
		return data, nil, ErrTicketInvalid
	}

	expectedVerifier, err := burnTokenVerifier(rootKey.Secret, burnToken)
	if err != nil {
		return data, nil, err
	}
	if subtle.ConstantTimeCompare(burnVerifier, expectedVerifier) != 1 {
		return data, nil, ErrTicketInvalid
	}
	return data, burnVerifier, nil
}

func signTicket(rootSecret []byte, data TicketData) (string, error) {
	payloadBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("error marshaling ticket: %w", err)
	}
	payload := b64Encode(payloadBytes)
	signedPayload := "v1." + payload
	signature, err := ticketSignature(rootSecret, []byte(signedPayload))
	if err != nil {
		return "", fmt.Errorf("error signing ticket: %w", err)
	}
	return signedPayload + "." + b64Encode(signature), nil
}

func ticketSignature(rootSecret []byte, signedPayload []byte) ([]byte, error) {
	key, err := deriveSubkey(rootSecret, "ticket signing v1")
	if err != nil {
		return nil, fmt.Errorf("error deriving ticket signing key: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(signedPayload)
	return mac.Sum(nil), nil
}

func validateTicketPayload(data TicketData, pathID NoteID, now time.Time) ([]byte, error) {
	if data.V != 1 || data.ID != pathID || data.IssuedAt.IsZero() || data.Exp.IsZero() {
		return nil, ErrTicketInvalid
	}
	if data.IssuedAt.After(now.Add(ticketClockSkew)) {
		return nil, ErrTicketInvalid
	}
	if data.Exp.Before(now) {
		return nil, ErrTicketExpired
	}
	if data.Exp.Sub(data.IssuedAt) > ticketTimeout {
		return nil, ErrTicketInvalid
	}
	verifier, err := b64Decode(data.BurnVerifier)
	if err != nil || len(verifier) != burnVerifierLen {
		return nil, ErrTicketInvalid
	}
	return verifier, nil
}
