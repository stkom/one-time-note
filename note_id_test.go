package main

import (
	"encoding/json"
	"testing"
)

func TestNewNoteID(t *testing.T) {
	id := NewNoteID()

	// Check if it's not all zeros (highly unlikely for 32 bytes)
	allZeros := true
	for _, b := range id {
		if b != 0 {
			allZeros = false
			break
		}
	}
	if allZeros {
		t.Error("NewNoteID returned all zeros")
	}

	// Check uniqueness
	id2 := NewNoteID()
	if id == id2 {
		t.Error("NewNoteID returned duplicate IDs")
	}
}

func TestNoteID_String_Parse(t *testing.T) {
	id := NewNoteID()
	s := id.String()

	parsed, err := ParseNoteID(s)
	if err != nil {
		t.Fatalf("ParseNoteID failed: %v", err)
	}

	if id != parsed {
		t.Errorf("Expected %v, got %v", id, parsed)
	}
}

func TestNoteID_MarshalUnmarshalText(t *testing.T) {
	id := NewNoteID()
	text, err := id.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText failed: %v", err)
	}

	var decoded NoteID
	err = decoded.UnmarshalText(text)
	if err != nil {
		t.Fatalf("UnmarshalText failed: %v", err)
	}

	if id != decoded {
		t.Errorf("Expected %v, got %v", id, decoded)
	}
}

func TestNoteID_JSON(t *testing.T) {
	id := NewNoteID()
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Check if it's a valid JSON string (should be wrapped in quotes)
	var s string
	err = json.Unmarshal(data, &s)
	if err != nil {
		t.Errorf("JSON output is not a valid JSON string: %v. Data: %s", err, string(data))
	}

	var decoded NoteID
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if id != decoded {
		t.Errorf("Expected %v, got %v", id, decoded)
	}
}

func TestParseNoteID_Errors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"invalid base64", "!!!!"},
		{"invalid length short", "YWJj"},
		{"invalid length long", "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXphYmNkZWZnaGlqa2xtbm9wcXJzdHV2d3h5eg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseNoteID(tt.in)
			if err == nil {
				t.Error("expected error for invalid input")
			}
		})
	}
}
