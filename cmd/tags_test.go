package cmd

import (
	"testing"
)

func TestParseTags_SingleTag(t *testing.T) {
	tags, err := parseTags([]string{"key=value"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0][0] != "key" || tags[0][1] != "value" {
		t.Errorf("expected [key, value], got %v", tags[0])
	}
}

func TestParseTags_MultipleTags(t *testing.T) {
	tags, err := parseTags([]string{"t=nostr", "t=bitcoin"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0][0] != "t" || tags[0][1] != "nostr" {
		t.Errorf("tag 0: expected [t, nostr], got %v", tags[0])
	}
	if tags[1][0] != "t" || tags[1][1] != "bitcoin" {
		t.Errorf("tag 1: expected [t, bitcoin], got %v", tags[1])
	}
}

func TestParseTags_SemicolonMultiValue(t *testing.T) {
	tags, err := parseTags([]string{"custom=a;b;c"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	tag := tags[0]
	if len(tag) != 4 {
		t.Fatalf("expected 4 elements [custom, a, b, c], got %d: %v", len(tag), tag)
	}
	if tag[0] != "custom" || tag[1] != "a" || tag[2] != "b" || tag[3] != "c" {
		t.Errorf("expected [custom, a, b, c], got %v", tag)
	}
}

func TestParseTags_JSONTags(t *testing.T) {
	tags, err := parseTags(nil, `[["t","bitcoin"],["p","hexkey"]]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0][0] != "t" || tags[0][1] != "bitcoin" {
		t.Errorf("tag 0: expected [t, bitcoin], got %v", tags[0])
	}
	if tags[1][0] != "p" || tags[1][1] != "hexkey" {
		t.Errorf("tag 1: expected [p, hexkey], got %v", tags[1])
	}
}

func TestParseTags_MergesTagAndTags(t *testing.T) {
	tags, err := parseTags([]string{"t=nostr"}, `[["p","hexkey"]]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	// --tag entries come first, then --tags JSON
	if tags[0][0] != "t" || tags[0][1] != "nostr" {
		t.Errorf("tag 0: expected [t, nostr], got %v", tags[0])
	}
	if tags[1][0] != "p" || tags[1][1] != "hexkey" {
		t.Errorf("tag 1: expected [p, hexkey], got %v", tags[1])
	}
}

func TestParseTags_EmptyInputs(t *testing.T) {
	tags, err := parseTags(nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

func TestParseTags_InvalidTagNoEquals(t *testing.T) {
	_, err := parseTags([]string{"invalidformat"}, "")
	if err == nil {
		t.Fatal("expected error for tag without =, got nil")
	}
}

func TestParseTags_InvalidTagsJSON(t *testing.T) {
	_, err := parseTags(nil, "not valid json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseTags_ValueWithEquals(t *testing.T) {
	tags, err := parseTags([]string{"url=https://example.com?a=b"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0][0] != "url" || tags[0][1] != "https://example.com?a=b" {
		t.Errorf("expected [url, https://example.com?a=b], got %v", tags[0])
	}
}

func TestParseTags_EmptySlice(t *testing.T) {
	tags, err := parseTags([]string{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}
