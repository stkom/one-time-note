package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
)

const (
	maxTicketLen         = 512
	minNoteLifetime      = time.Hour
	maxNoteLifetime      = 7 * 24 * time.Hour
	currentSchemaVersion = 1
	ticketClockSkew      = time.Minute
	ticketTimeout        = 15 * time.Minute
)

var (
	ErrNoteOpenFailed  = errors.New("note open failed")
	ErrPayloadEmpty    = errors.New("payload empty")
	ErrPayloadTooLarge = errors.New("payload too large")
	ErrStorageFull     = errors.New("storage full")
	ErrTicketExpired   = errors.New("ticket expired")
	ErrTicketInvalid   = errors.New("ticket invalid")
	ErrTicketUnusable  = errors.New("ticket unusable")
	ErrLifetimeInvalid = errors.New("note lifetime invalid")
)

var (
	payloadBucketName     = []byte("payloads")
	configBucketName      = []byte("config")
	metadataBucketName    = []byte("metadata")
	rootKeysBucketName    = []byte("root_keys")
	usedTicketsBucketName = []byte("used_tickets")

	schemaVersionKey = []byte("schema_version")
)

type Note struct {
	Payload  []byte
	Metadata Metadata
}

type Metadata struct {
	BurnVerifier []byte    `json:"burnVerifier"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type UsedTicketMarker struct {
	ExpiresAt time.Time `json:"expiresAt"`
}

type NoteService struct {
	db          *bolt.DB
	maxLifetime time.Duration
	maxSize     int
	now         func() time.Time
}

func NewNoteService(db *bolt.DB, maxSize int) *NoteService {
	return &NoteService{
		db:          db,
		maxLifetime: maxNoteLifetime,
		maxSize:     maxSize,
		now:         time.Now,
	}
}

func (s *NoteService) StartupMaintenance() error {
	if err := s.EnsureRootKeys(); err != nil {
		return err
	}
	if err := s.Cleanup(); err != nil {
		return err
	}
	return nil
}

func (s *NoteService) CreateNote(pathID NoteID, ticket string, burnToken BurnToken, payload []byte, lifetime time.Duration) (NoteID, error) {
	if len(ticket) > maxTicketLen {
		return NoteID{}, ErrTicketInvalid
	}
	if len(payload) == 0 {
		return NoteID{}, ErrPayloadEmpty
	}
	if len(payload) > s.maxSize {
		return NoteID{}, ErrPayloadTooLarge
	}
	lifetime, err := s.normalizeLifetime(lifetime)
	if err != nil {
		return NoteID{}, err
	}

	ticketData, burnVerifier, err := s.verifyTicket(ticket, pathID, burnToken)
	if err != nil {
		return NoteID{}, err
	}

	now := s.now()
	metadata := Metadata{
		BurnVerifier: burnVerifier,
		CreatedAt:    now,
		ExpiresAt:    now.Add(lifetime),
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return NoteID{}, fmt.Errorf("error marshaling metadata: %w", err)
	}
	markerBytes, err := json.Marshal(UsedTicketMarker{ExpiresAt: ticketData.Exp})
	if err != nil {
		return NoteID{}, fmt.Errorf("error marshaling used-ticket marker: %w", err)
	}

	id := ticketData.ID
	create := func() error {
		return s.db.Update(func(tx *bolt.Tx) error {
			usedTickets := tx.Bucket(usedTicketsBucketName)
			if usedTickets.Get(id[:]) != nil {
				return ErrTicketUnusable
			}
			if tx.Bucket(metadataBucketName).Get(id[:]) != nil || tx.Bucket(payloadBucketName).Get(id[:]) != nil {
				return ErrTicketUnusable
			}

			if err := tx.Bucket(metadataBucketName).Put(id[:], metadataBytes); err != nil {
				return fmt.Errorf("error saving metadata: %w", err)
			}
			if err := tx.Bucket(payloadBucketName).Put(id[:], payload); err != nil {
				return fmt.Errorf("error saving note payload: %w", err)
			}
			if err := usedTickets.Put(id[:], markerBytes); err != nil {
				return fmt.Errorf("error saving used-ticket marker: %w", err)
			}
			return nil
		})
	}

	err = create()
	if isStorageFull(err) {
		if cleanupErr := s.Cleanup(); cleanupErr != nil {
			return NoteID{}, cleanupErr
		}
		err = create()
	}
	if errors.Is(err, berrors.ErrMaxSizeReached) {
		return NoteID{}, ErrStorageFull
	}
	if err != nil {
		return NoteID{}, err
	}
	return id, nil
}

func (s *NoteService) OpenNote(id NoteID, burnToken BurnToken) (*Note, error) {
	now := s.now()
	var note *Note

	err := s.db.Update(func(tx *bolt.Tx) error {
		metadataBytes := tx.Bucket(metadataBucketName).Get(id[:])
		payload := tx.Bucket(payloadBucketName).Get(id[:])
		if metadataBytes == nil || payload == nil {
			return nil
		}

		var metadata Metadata
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			return deleteNoteRecords(tx, id)
		}
		if !s.validMetadata(metadata) {
			return deleteNoteRecords(tx, id)
		}

		rootKey, err := getRootKey(tx.Bucket(rootKeysBucketName))
		if err != nil {
			return err
		}
		verifier, err := burnTokenVerifier(rootKey.Secret, burnToken)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare(verifier, metadata.BurnVerifier) != 1 {
			return nil
		}

		if metadata.ExpiresAt.Before(now) {
			return deleteNoteRecords(tx, id)
		}

		payloadCopy := append([]byte(nil), payload...)
		note = &Note{Payload: payloadCopy, Metadata: metadata}
		return deleteNoteRecords(tx, id)
	})
	if err != nil {
		return nil, err
	}
	if note == nil {
		return nil, ErrNoteOpenFailed
	}
	return note, nil
}

func isStorageFull(err error) bool {
	return errors.Is(err, ErrStorageFull) || errors.Is(err, berrors.ErrMaxSizeReached)
}

func (s *NoteService) normalizeLifetime(lifetime time.Duration) (time.Duration, error) {
	if lifetime < minNoteLifetime || lifetime > s.maxLifetime {
		return 0, ErrLifetimeInvalid
	}
	return lifetime, nil
}

func (s *NoteService) validMetadata(metadata Metadata) bool {
	if metadata.CreatedAt.IsZero() || metadata.ExpiresAt.IsZero() {
		return false
	}
	if metadata.CreatedAt.After(metadata.ExpiresAt) || metadata.ExpiresAt.Sub(metadata.CreatedAt) > s.maxLifetime {
		return false
	}
	if len(metadata.BurnVerifier) != burnVerifierLen {
		return false
	}
	return true
}

func (s *NoteService) Cleanup() error {
	now := s.now()
	return s.db.Update(func(tx *bolt.Tx) error {
		metadata := tx.Bucket(metadataBucketName)
		var deleteIDs [][]byte
		if err := metadata.ForEach(func(k, v []byte) error {
			var item Metadata
			if err := json.Unmarshal(v, &item); err != nil {
				deleteIDs = append(deleteIDs, append([]byte(nil), k...))
				return nil
			}
			if !s.validMetadata(item) || item.ExpiresAt.Before(now) {
				deleteIDs = append(deleteIDs, append([]byte(nil), k...))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, idBytes := range deleteIDs {
			var id NoteID
			copy(id[:], idBytes)
			if err := deleteNoteRecords(tx, id); err != nil {
				return err
			}
		}

		usedTickets := tx.Bucket(usedTicketsBucketName)
		var expiredTickets [][]byte
		if err := usedTickets.ForEach(func(k, v []byte) error {
			var marker UsedTicketMarker
			if err := json.Unmarshal(v, &marker); err != nil || marker.ExpiresAt.Before(now) {
				expiredTickets = append(expiredTickets, append([]byte(nil), k...))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, key := range expiredTickets {
			_ = usedTickets.Delete(key)
		}
		return nil
	})
}

func deleteNoteRecords(tx *bolt.Tx, id NoteID) error {
	payloads := tx.Bucket(payloadBucketName)
	if err := payloads.Delete(id[:]); err != nil {
		return fmt.Errorf("error deleting payload: %w", err)
	}
	if err := tx.Bucket(metadataBucketName).Delete(id[:]); err != nil {
		return fmt.Errorf("error deleting metadata: %w", err)
	}
	return nil
}
