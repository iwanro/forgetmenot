package embed

import "testing"

func TestBaseURLTrailingSlash(t *testing.T) {
	o := NewOllama("http://localhost:11434/", "nomic-embed-text")
	if o.BaseURL != "http://localhost:11434" {
		t.Fatalf("ollama BaseURL = %q, want trailing slash trimmed", o.BaseURL)
	}
	c := NewOpenAICompat("https://api.openai.com/v1/", "k", "m")
	if c.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("openai BaseURL = %q, want trailing slash trimmed", c.BaseURL)
	}
}
