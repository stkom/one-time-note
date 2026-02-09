package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	bolt "go.etcd.io/bbolt"
)

var rootKeyStorageKey = []byte("root_key")

type rootKeyRecord struct {
	CreatedAt time.Time `json:"createdAt"`
	Secret    []byte    `json:"secret"`
}

func (s *NoteService) EnsureRootKeys() error {
	now := s.now()
	return s.db.Update(func(tx *bolt.Tx) error {
		keys := tx.Bucket(rootKeysBucketName)
		if keys.Get(rootKeyStorageKey) != nil {
			_, err := getRootKey(keys)
			return err
		}

		secret := make([]byte, 32)
		fillRandom(secret)
		if err := putRootKey(keys, rootKeyRecord{CreatedAt: now, Secret: secret}); err != nil {
			return err
		}
		slog.Info(logMsgKeyManagement, "event", "root_key_created", "created_at", now)
		return nil
	})
}

func putRootKey(bucket *bolt.Bucket, record rootKeyRecord) error {
	if len(record.Secret) != 32 || record.CreatedAt.IsZero() {
		return errors.New("invalid root key record")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("error marshaling root key record: %w", err)
	}
	if err := bucket.Put(rootKeyStorageKey, data); err != nil {
		return fmt.Errorf("error saving root key record: %w", err)
	}
	return nil
}

func getRootKey(bucket *bolt.Bucket) (rootKeyRecord, error) {
	var record rootKeyRecord
	data := bucket.Get(rootKeyStorageKey)
	if data == nil {
		return record, errors.New("root key not found")
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, fmt.Errorf("error parsing root key: %w", err)
	}
	if len(record.Secret) != 32 || record.CreatedAt.IsZero() {
		return record, errors.New("root key is invalid")
	}
	record.Secret = append([]byte(nil), record.Secret...)
	return record, nil
}
