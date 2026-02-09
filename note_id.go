package main

import "fmt"

const noteIDLen = 32

type NoteID [noteIDLen]byte

func NewNoteID() NoteID {
	var id NoteID
	fillRandom(id[:])
	return id
}

func ParseNoteID(s string) (NoteID, error) {
	var id NoteID
	return id, id.UnmarshalText([]byte(s))
}

func (id NoteID) String() string {
	return b64Encode(id[:])
}

func (id NoteID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

//goland:noinspection GoMixedReceiverTypes
func (id *NoteID) UnmarshalText(text []byte) error {
	decoded, err := b64Decode(string(text))
	if err != nil {
		return fmt.Errorf("invalid note ID: %w", err)
	}
	if len(decoded) != noteIDLen {
		return fmt.Errorf("invalid note ID length: %d", len(decoded))
	}
	copy(id[:], decoded)
	return nil
}
