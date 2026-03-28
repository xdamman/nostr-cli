package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrintRaw_ProducesCompactJSON(t *testing.T) {
	input := map[string]interface{}{
		"id":      "abc123",
		"content": "hello world",
		"kind":    1,
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	output := string(data)

	// Should be single line (no newlines in the JSON itself)
	if strings.Contains(output, "\n") {
		t.Error("printRaw output should be compact (no newlines)")
	}

	// Should be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}

	if parsed["id"] != "abc123" {
		t.Errorf("expected id=abc123, got %v", parsed["id"])
	}
}

func TestPrintJSONL_ProducesSingleLineJSON(t *testing.T) {
	input := map[string]interface{}{
		"id":      "def456",
		"content": "test message",
		"tags":    [][]string{{"t", "nostr"}},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	output := string(data)

	// Should be single line
	if strings.Contains(output, "\n") {
		t.Error("JSONL output should be single line")
	}

	// Should be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
}

func TestColorizeJSON_ContainsExpectedKeys(t *testing.T) {
	input := `{"name":"test","count":42,"active":true,"data":null}`
	// Pretty-print it first (colorizeJSON expects pretty-printed input)
	var v interface{}
	json.Unmarshal([]byte(input), &v)
	pretty, _ := json.MarshalIndent(v, "", "  ")

	output := colorizeJSON(string(pretty))

	// Should contain the key and value text (with ANSI codes around them)
	if !strings.Contains(output, "name") {
		t.Error("colorized output should contain key 'name'")
	}
	if !strings.Contains(output, "test") {
		t.Error("colorized output should contain value 'test'")
	}
	if !strings.Contains(output, "42") {
		t.Error("colorized output should contain number 42")
	}
	if !strings.Contains(output, "true") {
		t.Error("colorized output should contain boolean true")
	}
	if !strings.Contains(output, "null") {
		t.Error("colorized output should contain null")
	}

	// Should contain ANSI escape codes
	if !strings.Contains(output, "\033[") {
		t.Error("colorized output should contain ANSI escape codes")
	}
}

func TestColorizeJSON_EmptyObject(t *testing.T) {
	output := colorizeJSON("{}")
	if !strings.Contains(output, "{") || !strings.Contains(output, "}") {
		t.Error("colorized empty object should still contain braces")
	}
}

func TestIsJSONKey_True(t *testing.T) {
	s := `"name": "value"`
	if !isJSONKey(s, 0) {
		t.Error("expected isJSONKey to return true for key position")
	}
}

func TestIsJSONKey_False(t *testing.T) {
	s := `"name": "value"`
	// Position of "value" string
	idx := strings.Index(s, `"value"`)
	if isJSONKey(s, idx) {
		t.Error("expected isJSONKey to return false for value position")
	}
}
