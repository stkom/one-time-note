package main

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openDB(cfg *Config) (*bolt.DB, error) {
	parent := filepath.Dir(cfg.DBPath)
	if err := validateDBParent(parent); err != nil {
		return nil, err
	}
	if cfg.MaxDBSize > math.MaxInt {
		return nil, fmt.Errorf("%s is too large for this platform", envMaxDBSize)
	}
	db, err := bolt.Open(cfg.DBPath, 0600, &bolt.Options{Timeout: 5 * time.Second, MaxSize: cfg.MaxDBSize})
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}
	if err := validateDBFile(cfg.DBPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if err := ensureBuckets(tx); err != nil {
			return err
		}
		if err := ensureSchemaVersion(tx); err != nil {
			return err
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureBuckets(tx *bolt.Tx) error {
	for _, bucket := range [][]byte{payloadBucketName, configBucketName, metadataBucketName, rootKeysBucketName, usedTicketsBucketName} {
		if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
			return fmt.Errorf("error creating bucket %q: %w", bucket, err)
		}
	}
	return nil
}

func ensureSchemaVersion(tx *bolt.Tx) error {
	config := tx.Bucket(configBucketName)
	version := decodeUint64(config.Get(schemaVersionKey))
	if version == 0 {
		if err := config.Put(schemaVersionKey, encodeUint64(currentSchemaVersion)); err != nil {
			return err
		}
		return nil
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("unsupported database schema version %d", version)
	}
	if version < currentSchemaVersion {
		return fmt.Errorf("unsupported database schema version %d", version)
	}
	return nil
}

func validateDBParent(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("error checking database directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("database parent path %q is not a directory", path)
	}
	perm := info.Mode().Perm()
	if perm&0002 != 0 && info.Mode()&os.ModeSticky == 0 {
		slog.Warn(logMsgDatabasePerms, "event", "database_directory_world_writable", "path", path, "mode", perm.String())
	}
	return nil
}

func validateDBFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("error checking database file: %w", err)
	}
	if info.Mode().Perm()&0077 != 0 {
		slog.Warn(logMsgDatabasePerms, "event", "database_file_group_or_other_accessible", "path", path, "mode", info.Mode().Perm().String())
	}
	return nil
}

func encodeUint64(value uint64) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, value)
	return key
}

func decodeUint64(value []byte) uint64 {
	if len(value) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}
