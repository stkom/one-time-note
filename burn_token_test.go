package main

import (
	"encoding/json"
	"testing"
)

func TestBurnToken_String_Parse(t *testing.T) {
	token := NewBurnToken()
	s := token.String()

	parsed, err := ParseBurnToken(s)
	if err != nil {
		t.Fatalf("ParseBurnToken failed: %v", err)
	}

	if token != parsed {
		t.Errorf("Expected %v, got %v", token, parsed)
	}
}

func TestBurnToken_MarshalUnmarshalText(t *testing.T) {
	token := NewBurnToken()
	text, err := token.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText failed: %v", err)
	}

	var decoded BurnToken
	err = decoded.UnmarshalText(text)
	if err != nil {
		t.Fatalf("UnmarshalText failed: %v", err)
	}

	if token != decoded {
		t.Errorf("Expected %v, got %v", token, decoded)
	}
}

func TestBurnToken_JSON(t *testing.T) {
	token := NewBurnToken()
	data, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var s string
	err = json.Unmarshal(data, &s)
	if err != nil {
		t.Errorf("JSON output is not a valid JSON string: %v. Data: %s", err, string(data))
	}

	var decoded BurnToken
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if token != decoded {
		t.Errorf("Expected %v, got %v", token, decoded)
	}
}

func TestParseBurnToken_Errors(t *testing.T) {
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
			_, err := ParseBurnToken(tt.in)
			if err == nil {
				t.Error("expected error for invalid input")
			}
		})
	}
}
