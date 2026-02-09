package main

import (
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestOpenDatabaseAppliesConfiguredMaxDBSize(t *testing.T) {
	cfg := &Config{
		DBPath:    filepath.Join(t.TempDir(), "notes.db"),
		MaxDBSize: 2 * 1024 * 1024,
	}

	db, err := openDB(cfg)
	if err != nil {
		t.Fatalf("openDatabase returned error: %v", err)
	}
	defer db.Close()

	if db.MaxSize != int(cfg.MaxDBSize) {
		t.Fatalf("db.MaxSize = %d, want %d", db.MaxSize, cfg.MaxDBSize)
	}
}

func TestOpenDatabaseInitializesBucketsAndSchemaVersion(t *testing.T) {
	cfg := &Config{
		DBPath:    filepath.Join(t.TempDir(), "notes.db"),
		MaxDBSize: defaultMaxDBSize,
	}

	db, err := openDB(cfg)
	if err != nil {
		t.Fatalf("openDatabase returned error: %v", err)
	}
	defer db.Close()

	err = db.View(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{payloadBucketName, configBucketName, metadataBucketName, rootKeysBucketName, usedTicketsBucketName} {
			if tx.Bucket(bucket) == nil {
				t.Fatalf("bucket %q was not created", bucket)
			}
		}
		got := decodeUint64(tx.Bucket(configBucketName).Get(schemaVersionKey))
		if got != currentSchemaVersion {
			t.Fatalf("schema version = %d, want %d", got, currentSchemaVersion)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenDatabaseRejectsFutureSchemaVersion(t *testing.T) {
	cfg := &Config{
		DBPath:    filepath.Join(t.TempDir(), "notes.db"),
		MaxDBSize: defaultMaxDBSize,
	}

	db, err := openDB(cfg)
	if err != nil {
		t.Fatalf("openDatabase returned error: %v", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(configBucketName).Put(schemaVersionKey, encodeUint64(currentSchemaVersion+1))
	})
	if err != nil {
		db.Close()
		t.Fatalf("failed to write future schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close database: %v", err)
	}

	db, err = openDB(cfg)
	if err == nil {
		db.Close()
		t.Fatal("openDatabase returned nil error for future schema version")
	}
	if !strings.Contains(err.Error(), "unsupported database schema version") {
		t.Fatalf("openDatabase error = %v, want unsupported schema version", err)
	}
}
