package cmd

import "testing"

// ---------------------------------------------------------------------------
// slugify
// ---------------------------------------------------------------------------

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"My Blog Post", "my-blog-post"},
		{"Hello World!", "hello-world"},
		{"  spaces  ", "spaces"},
		{"UPPERCASE", "uppercase"},
		{"special@chars#here", "special-chars-here"},
		{"a/b/c", "a-b-c"},
		{"", ""},
		{"already-slugged", "already-slugged"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := slugify(tt.in)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSlugify_TruncatesAt80(t *testing.T) {
	long := "this is a very long title that should get truncated at eighty characters because it exceeds that limit"
	got := slugify(long)
	if len(got) > 80 {
		t.Errorf("slugify returned %d chars, want <= 80", len(got))
	}
}

// ---------------------------------------------------------------------------
// parseFrontmatter
// ---------------------------------------------------------------------------

func TestParseFrontmatter_Full(t *testing.T) {
	input := `---
title: My Blog Post
summary: A great article about Nostr
image: https://example.com/header.jpg
slug: my-blog-post
hashtags: [nostr, protocol]
draft: true
---

# My Blog Post

Content here...
`
	fm, body := parseFrontmatter(input)
	if fm == nil {
		t.Fatal("expected frontmatter, got nil")
	}
	if fm.Title != "My Blog Post" {
		t.Errorf("title = %q, want %q", fm.Title, "My Blog Post")
	}
	if fm.Summary != "A great article about Nostr" {
		t.Errorf("summary = %q", fm.Summary)
	}
	if fm.Image != "https://example.com/header.jpg" {
		t.Errorf("image = %q", fm.Image)
	}
	if fm.Slug != "my-blog-post" {
		t.Errorf("slug = %q", fm.Slug)
	}
	if !fm.Draft {
		t.Error("expected draft = true")
	}
	if len(fm.Hashtags) != 2 || fm.Hashtags[0] != "nostr" || fm.Hashtags[1] != "protocol" {
		t.Errorf("hashtags = %v", fm.Hashtags)
	}
	if body != "# My Blog Post\n\nContent here...\n" {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatter_None(t *testing.T) {
	input := "# Just content\n\nNo frontmatter here."
	fm, body := parseFrontmatter(input)
	if fm != nil {
		t.Error("expected nil frontmatter")
	}
	if body != input {
		t.Errorf("body should be unchanged")
	}
}

func TestParseFrontmatter_Empty(t *testing.T) {
	input := "---\n---\n\nContent"
	fm, body := parseFrontmatter(input)
	if fm == nil {
		t.Fatal("expected non-nil frontmatter (empty block)")
	}
	if fm.Title != "" || fm.Slug != "" {
		t.Error("expected empty fields")
	}
	if body != "Content" {
		t.Errorf("body = %q, want %q", body, "Content")
	}
}

func TestParseFrontmatter_QuotedValues(t *testing.T) {
	input := "---\ntitle: \"Quoted Title\"\nsummary: 'Single Quoted'\n---\nBody"
	fm, _ := parseFrontmatter(input)
	if fm == nil {
		t.Fatal("expected frontmatter")
	}
	if fm.Title != "Quoted Title" {
		t.Errorf("title = %q, want %q", fm.Title, "Quoted Title")
	}
	if fm.Summary != "Single Quoted" {
		t.Errorf("summary = %q, want %q", fm.Summary, "Single Quoted")
	}
}

// ---------------------------------------------------------------------------
// Contract tests: NIP-23 flags exist
// ---------------------------------------------------------------------------

func TestLLM_Post_LongFormFlags(t *testing.T) {
	cmd := requireCmd(t, "post")
	flags := []string{"file", "long", "title", "summary", "image", "slug", "draft", "hashtag"}
	for _, f := range flags {
		t.Run("--"+f, func(t *testing.T) {
			requireFlag(t, cmd, f)
		})
	}
}

func TestLLM_Post_FileHasShortFlag(t *testing.T) {
	cmd := requireCmd(t, "post")
	f := cmd.Flags().Lookup("file")
	if f == nil {
		t.Fatal("--file flag not found")
	}
	if f.Shorthand != "f" {
		t.Errorf("--file shorthand = %q, want %q", f.Shorthand, "f")
	}
}
