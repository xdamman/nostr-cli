package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nbd-wtf/go-nostr"
)

// parseTags merges --tag key=value entries and --tags JSON into a tag slice.
// For --tag: split on "=", first part is tag name, rest is value.
// Semicolons in the value create multi-element tags:
//
//	--tag custom="val1;val2;val3" → ["custom", "val1", "val2", "val3"]
//
// For --tags: parse as JSON array of string arrays:
//
//	--tags '[["t","bitcoin"],["p","<hex>"]]'
func parseTags(tagFlags []string, tagsJSON string) (nostr.Tags, error) {
	var tags nostr.Tags

	for _, t := range tagFlags {
		parts := strings.SplitN(t, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid tag format %q (expected key=value)", t)
		}
		key := parts[0]
		values := strings.Split(parts[1], ";")
		tag := nostr.Tag{key}
		tag = append(tag, values...)
		tags = append(tags, tag)
	}

	if tagsJSON != "" {
		var jsonTags [][]string
		if err := json.Unmarshal([]byte(tagsJSON), &jsonTags); err != nil {
			return nil, fmt.Errorf("invalid --tags JSON: %w", err)
		}
		for _, jt := range jsonTags {
			tags = append(tags, nostr.Tag(jt))
		}
	}

	return tags, nil
}
