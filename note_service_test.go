package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

const testNoteLifetime = 2 * time.Hour

func TestCreateAndOpenNote(t *testing.T) {
	service, _, cleanup := setupTestService(t, 1024)
	defer cleanup()

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	payload := testPayloadBytes("secret message")

	id, err := service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, payload, testNoteLifetime)
	if err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	if id != ticket.Data.ID {
		t.Fatalf("id = %v, want %v", id, ticket.Data.ID)
	}
	note, err := service.OpenNote(id, ticket.BurnToken)
	if err != nil {
		t.Fatalf("OpenNote returned error: %v", err)
	}
	if string(note.Payload) != string(payload) {
		t.Fatalf("payload = %q, want %q", note.Payload, payload)
	}

	_, err = service.OpenNote(id, ticket.BurnToken)
	if !errors.Is(err, ErrNoteOpenFailed) {
		t.Fatalf("second OpenNote error = %v, want ErrNoteOpenFailed", err)
	}
}

func TestCreateNoteUsesRequestedLifetime(t *testing.T) {
	service, db, cleanup := setupTestService(t, 1024)
	defer cleanup()
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	lifetime := 90*time.Minute + time.Second
	id, err := service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), lifetime)
	if err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}

	metadata := readMetadata(t, db, id)
	if !metadata.ExpiresAt.Equal(now.Add(lifetime)) {
		t.Fatalf("ExpiresAt = %s, want %s", metadata.ExpiresAt, now.Add(lifetime))
	}
}

func TestCreateNoteRejectsInvalidLifetime(t *testing.T) {
	service, _, cleanup := setupTestService(t, 1024)
	defer cleanup()

	for _, lifetime := range []time.Duration{0, -time.Second, time.Minute, maxNoteLifetime + time.Second} {
		t.Run(lifetime.String(), func(t *testing.T) {
			ticket, err := service.CreateTicket()
			if err != nil {
				t.Fatalf("CreateTicket returned error: %v", err)
			}
			_, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), lifetime)
			if !errors.Is(err, ErrLifetimeInvalid) {
				t.Fatalf("CreateNote error = %v, want ErrLifetimeInvalid", err)
			}
		})
	}
}

func TestOpenNoteRequiresBurnTokenAndDoesNotBurnOnInvalidToken(t *testing.T) {
	service, _, cleanup := setupTestService(t, 1024)
	defer cleanup()

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	if _, err := service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime); err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}

	_, err = service.OpenNote(ticket.Data.ID, NewBurnToken())
	if !errors.Is(err, ErrNoteOpenFailed) {
		t.Fatalf("OpenNote with wrong token error = %v, want ErrNoteOpenFailed", err)
	}

	if _, err = service.OpenNote(ticket.Data.ID, ticket.BurnToken); err != nil {
		t.Fatalf("OpenNote with correct token after failed attempt returned error: %v", err)
	}
}

func TestCreateNoteRejectsMismatchedPathIDAndDuplicateTicket(t *testing.T) {
	service, _, cleanup := setupTestService(t, 1024)
	defer cleanup()

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	_, err = service.CreateNote(ticket.Data.ID, strings.Repeat("x", maxTicketLen+1), ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime)
	if !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("CreateNote with oversized ticket error = %v, want ErrTicketInvalid", err)
	}

	_, err = service.CreateNote(NewNoteID(), ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime)
	if !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("CreateNote with mismatched ID error = %v, want ErrTicketInvalid", err)
	}

	if _, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime); err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	_, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime)
	if !errors.Is(err, ErrTicketUnusable) {
		t.Fatalf("duplicate CreateNote error = %v, want ErrTicketUnusable", err)
	}
}

func TestCreateNoteEnforcesPayloadLimits(t *testing.T) {
	firstPayload := testPayloadBytes("one")
	secondPayload := testPayloadBytes("two")
	service, _, cleanup := setupTestService(t, 1024)
	defer cleanup()

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	_, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, nil, testNoteLifetime)
	if !errors.Is(err, ErrPayloadEmpty) {
		t.Fatalf("empty payload error = %v, want ErrPayloadEmpty", err)
	}

	largePayload := testPayloadBytes(strings.Repeat("x", 2000))
	_, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, largePayload, testNoteLifetime)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("large payload error = %v, want ErrPayloadTooLarge", err)
	}

	first, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	if _, err = service.CreateNote(first.Data.ID, first.Value, first.BurnToken, firstPayload, testNoteLifetime); err != nil {
		t.Fatalf("first CreateNote returned error: %v", err)
	}
	second, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	if _, err = service.OpenNote(first.Data.ID, first.BurnToken); err != nil {
		t.Fatalf("OpenNote returned error: %v", err)
	}
	if _, err = service.CreateNote(second.Data.ID, second.Value, second.BurnToken, secondPayload, testNoteLifetime); err != nil {
		t.Fatalf("CreateNote after opening stored note returned error: %v", err)
	}
}

func TestCreateNoteReturnsStorageFullWhenDatabaseMaxSizeReached(t *testing.T) {
	service, _, cleanup := setupTestServiceWithMaxDBSize(t, 256*1024, 512*1024)
	defer cleanup()

	payload := testPayloadBytes(strings.Repeat("x", 64*1024))
	for created := 0; created < 20; created++ {
		ticket, err := service.CreateTicket()
		if err != nil {
			t.Fatalf("CreateTicket returned error: %v", err)
		}
		_, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, payload, testNoteLifetime)
		if errors.Is(err, ErrStorageFull) {
			if created == 0 {
				t.Fatal("CreateNote returned ErrStorageFull before storing any notes")
			}
			return
		}
		if err != nil {
			t.Fatalf("CreateNote returned error after %d successful notes: %v", created, err)
		}
	}
	t.Fatal("CreateNote did not return ErrStorageFull")
}

func TestOpenExpiredNoteWithValidBurnTokenDeletesPayload(t *testing.T) {
	service, db, cleanup := setupTestService(t, 1024)
	defer cleanup()

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	if _, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime); err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}

	if err = expireNoteMetadata(db, ticket.Data.ID); err != nil {
		t.Fatalf("expireNoteMetadata returned error: %v", err)
	}

	_, err = service.OpenNote(ticket.Data.ID, ticket.BurnToken)
	if !errors.Is(err, ErrNoteOpenFailed) {
		t.Fatalf("OpenNote error = %v, want ErrNoteOpenFailed", err)
	}
	err = db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(payloadBucketName).Get(ticket.Data.ID[:]) != nil {
			t.Fatal("expired note payload was not deleted after valid burn token")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenNoteDeletesInvalidMetadataAsGenericFailure(t *testing.T) {
	service, db, cleanup := setupTestService(t, 1024)
	defer cleanup()

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	if _, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime); err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	if err = writeInvalidMetadata(db, ticket.Data.ID); err != nil {
		t.Fatalf("writeInvalidMetadata returned error: %v", err)
	}

	_, err = service.OpenNote(ticket.Data.ID, ticket.BurnToken)
	if !errors.Is(err, ErrNoteOpenFailed) {
		t.Fatalf("OpenNote error = %v, want ErrNoteOpenFailed", err)
	}
	err = db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(payloadBucketName).Get(ticket.Data.ID[:]) != nil {
			t.Fatal("invalid metadata did not delete payload")
		}
		if tx.Bucket(metadataBucketName).Get(ticket.Data.ID[:]) != nil {
			t.Fatal("invalid metadata was not deleted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCleanupRemovesExpiredNotesAndMarkers(t *testing.T) {
	service, db, cleanup := setupTestService(t, 1024)
	defer cleanup()

	ticket, err := service.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	if _, err = service.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime); err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	if err = expireNoteMetadata(db, ticket.Data.ID); err != nil {
		t.Fatalf("expireNoteMetadata returned error: %v", err)
	}
	if err = expireUsedTicketMarker(db, ticket.Data.ID); err != nil {
		t.Fatalf("expireUsedTicketMarker returned error: %v", err)
	}

	if err = service.Cleanup(); err != nil {
		t.Fatalf("Cleanup returned error: %v", err)
	}

	err = db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(payloadBucketName).Get(ticket.Data.ID[:]) != nil {
			t.Fatal("expired payload was not deleted")
		}
		if tx.Bucket(metadataBucketName).Get(ticket.Data.ID[:]) != nil {
			t.Fatal("expired metadata was not deleted")
		}
		if tx.Bucket(usedTicketsBucketName).Get(ticket.Data.ID[:]) != nil {
			t.Fatal("expired used-ticket marker was not deleted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnsureRootKeysKeepsExistingRootKey(t *testing.T) {
	service, db, cleanup := setupTestService(t, 1024)
	defer cleanup()

	var original []byte
	err := db.Update(func(tx *bolt.Tx) error {
		key, err := getRootKey(tx.Bucket(rootKeysBucketName))
		if err != nil {
			return err
		}
		original = append([]byte(nil), key.Secret...)
		key.CreatedAt = time.Now().Add(-365 * 24 * time.Hour)
		return putRootKey(tx.Bucket(rootKeysBucketName), key)
	})
	if err != nil {
		t.Fatalf("failed to update root key: %v", err)
	}

	if err = service.EnsureRootKeys(); err != nil {
		t.Fatalf("EnsureRootKeys returned error: %v", err)
	}

	err = db.View(func(tx *bolt.Tx) error {
		key, err := getRootKey(tx.Bucket(rootKeysBucketName))
		if err != nil {
			t.Fatal(err)
		}
		if string(key.Secret) != string(original) {
			t.Fatal("root key changed unexpectedly")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testPayload(text string) string {
	return `{"text":"` + text + `"}`
}

func testPayloadBytes(text string) []byte {
	return []byte(testPayload(text))
}

func readMetadata(t *testing.T, db *bolt.DB, id NoteID) Metadata {
	t.Helper()
	var metadata Metadata
	err := db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(metadataBucketName).Get(id[:])
		if data == nil {
			t.Fatal("metadata was not stored")
		}
		return json.Unmarshal(data, &metadata)
	})
	if err != nil {
		t.Fatal(err)
	}
	return metadata
}

func expireNoteMetadata(db *bolt.DB, id NoteID) error {
	return db.Update(func(tx *bolt.Tx) error {
		var metadata Metadata
		if err := json.Unmarshal(tx.Bucket(metadataBucketName).Get(id[:]), &metadata); err != nil {
			return err
		}
		metadata.CreatedAt = time.Now().Add(-25 * time.Hour)
		metadata.ExpiresAt = time.Now().Add(-time.Hour)
		data, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		return tx.Bucket(metadataBucketName).Put(id[:], data)
	})
}

func writeInvalidMetadata(db *bolt.DB, id NoteID) error {
	return db.Update(func(tx *bolt.Tx) error {
		data, err := json.Marshal(Metadata{
			BurnVerifier: []byte("short"),
			CreatedAt:    time.Now(),
			ExpiresAt:    time.Now().Add(testNoteLifetime),
		})
		if err != nil {
			return err
		}
		return tx.Bucket(metadataBucketName).Put(id[:], data)
	})
}

func expireUsedTicketMarker(db *bolt.DB, id NoteID) error {
	return db.Update(func(tx *bolt.Tx) error {
		data, err := json.Marshal(UsedTicketMarker{ExpiresAt: time.Now().Add(-time.Hour)})
		if err != nil {
			return err
		}
		return tx.Bucket(usedTicketsBucketName).Put(id[:], data)
	})
}
