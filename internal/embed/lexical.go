// Package embed: lexical.go implements a deterministic, dependency-free
// embedding provider. It powers the offline fallback so remember/recall keep
// working when no Ollama or OpenAI-compatible endpoint is reachable: every
// agent environment (Claude Code, Cursor, opencode, ...) gets working memory
// with zero configuration.
package embed

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

// DefaultLexicalDim matches the most common local model (nomic-embed-text,
// 768 dims). The exact value does not matter for correctness: recall heals
// dimension mismatches by re-embedding, and provenance in memory metadata
// separates lexical from semantic vectors even at equal dimensions.
const DefaultLexicalDim = 768

// LexicalEmbedder hashes word unigrams and bigrams into a fixed-size vector.
// It is deterministic across runs and machines (no randomness), so memories
// written offline stay comparable later.
type LexicalEmbedder struct {
	dim int
}

// NewLexical returns a LexicalEmbedder. An optional positive dimension
// overrides DefaultLexicalDim.
func NewLexical(dim ...int) *LexicalEmbedder {
	d := DefaultLexicalDim
	if len(dim) > 0 && dim[0] > 0 {
		d = dim[0]
	}
	return &LexicalEmbedder{dim: d}
}

// Embed hashes each text into a normalized vector.
func (l *LexicalEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = l.vector(t)
	}
	return out, nil
}

// IsLexical marks this provider so the memory service calibrates its
// thresholds (recall floor, dedupe, conflict) to lexical similarity values.
func (l *LexicalEmbedder) IsLexical() bool { return true }

func (l *LexicalEmbedder) vector(text string) []float64 {
	v := make([]float64, l.dim)
	toks := lexicalTokens(text)
	if len(toks) == 0 {
		return v
	}
	add := func(feature string) {
		h := fnv.New64a()
		h.Write([]byte(feature))
		sum := h.Sum64()
		sign := 1.0
		if (sum>>63)&1 == 1 {
			sign = -1.0
		}
		v[sum%uint64(l.dim)] += sign
	}
	for _, t := range toks {
		add(t)
	}
	// Word-pair features capture short phrases ("jwt token" != "token jwt"
	// in most cases) and give exact bigram matches a strong boost.
	for i := 0; i+1 < len(toks); i++ {
		add(toks[i] + "\x00" + toks[i+1])
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	if norm > 0 {
		norm = math.Sqrt(norm)
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}

// lexicalTokens lowercases the text and splits it on anything that is not a
// letter or digit, dropping stopwords and single-character tokens. Digits are
// kept ("postgres 16" matters), so are hyphen-split parts ("python:3.12-slim"
// -> python, 3, 12, slim).
func lexicalTokens(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	toks := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 2 || englishStopwords[f] {
			continue
		}
		toks = append(toks, f)
	}
	return toks
}

// englishStopwords are dropped from lexical features. They carry no memory
// content and would otherwise inflate similarity between unrelated texts
// ("the" is in every sentence).
var englishStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"if": true, "then": true, "else": true, "when": true, "while": true,
	"for": true, "of": true, "to": true, "in": true, "on": true, "at": true,
	"by": true, "with": true, "from": true, "as": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"it": true, "its": true, "this": true, "that": true, "these": true,
	"those": true, "i": true, "you": true, "he": true, "she": true, "we": true,
	"they": true, "them": true, "his": true, "her": true, "their": true,
	"my": true, "your": true, "our": true, "not": true, "no": true, "yes": true,
	"so": true, "do": true, "does": true, "did": true, "have": true, "has": true,
	"had": true, "will": true, "would": true, "can": true, "could": true,
	"should": true, "may": true, "might": true, "must": true, "about": true,
	"into": true, "over": true, "after": true, "before": true, "between": true,
	"out": true, "up": true, "down": true, "again": true, "further": true,
	"once": true, "here": true, "there": true, "all": true, "any": true,
	"both": true, "each": true, "few": true, "more": true, "most": true,
	"other": true, "some": true, "such": true, "only": true, "own": true,
	"same": true, "than": true, "too": true, "very": true, "just": true,
	"because": true, "until": true, "during": true, "without": true,
	"against": true, "through": true, "also": true, "used": true,
	"use": true, "using": true, "via": true, "per": true, "e": true, "g": true,
	"eg": true, "ie": true, "vs": true,
}
