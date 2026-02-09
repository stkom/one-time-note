package main

import (
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func setupTestDBWithMaxDBSize(t *testing.T, maxDBSize int) (*bolt.DB, func()) {
	cfg := &Config{
		DBPath:    filepath.Join(t.TempDir(), "notes.db"),
		MaxDBSize: maxDBSize,
	}
	db, err := openDB(cfg)
	if err != nil {
		t.Fatalf("could not open database: %v", err)
	}

	return db, func() {
		db.Close()
	}
}

func setupTestService(t *testing.T, maxSize int) (*NoteService, *bolt.DB, func()) {
	return setupTestServiceWithMaxDBSize(t, maxSize, defaultMaxDBSize)
}

func setupTestServiceWithMaxDBSize(t *testing.T, maxSize int, maxDBSize int) (*NoteService, *bolt.DB, func()) {
	db, cleanup := setupTestDBWithMaxDBSize(t, maxDBSize)
	service := NewNoteService(db, maxSize)
	if err := service.StartupMaintenance(); err != nil {
		cleanup()
		t.Fatalf("could not initialize test service: %v", err)
	}
	return service, db, cleanup
}
