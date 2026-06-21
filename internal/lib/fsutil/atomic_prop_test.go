package fsutil

import (
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"
)

// **Validates: Requirements 11.5, 11.6**

// Property 30: Sanitization is filesystem-safe, Unicode-preserving, non-empty

// containsReservedChar returns true if s contains any character that is reserved
// or invalid in filesystem path components.
func containsReservedChar(s string) bool {
	for _, r := range s {
		if r == 0 {
			return true
		}
		if unicode.IsControl(r) {
			return true
		}
		if strings.ContainsRune(reservedChars, r) {
			return true
		}
	}
	return false
}

// nonEmptyFallback generates a non-empty fallback string for use in tests.
func nonEmptyFallback() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9_]{1,20}`)
}

func TestProperty30_OutputNeverContainsReservedChars(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.String().Draw(t, "name")
		fallback := nonEmptyFallback().Draw(t, "fallback")

		result := SanitizeComponent(name, fallback)

		if containsReservedChar(result) {
			t.Fatalf("output %q contains reserved/invalid filesystem characters for input %q", result, name)
		}
	})
}

func TestProperty30_CyrillicCharactersPreserved(t *testing.T) {
	// Generator that produces strings with Cyrillic characters mixed with valid ASCII.
	cyrillicMixed := rapid.Custom(func(t *rapid.T) string {
		// Generate a mix of Cyrillic and ASCII characters (no reserved chars).
		parts := rapid.SliceOfN(rapid.OneOf(
			rapid.StringMatching(`[а-яА-ЯёЁ]{1,5}`),
			rapid.StringMatching(`[a-zA-Z0-9 ]{1,5}`),
		), 2, 6).Draw(t, "parts")
		return strings.Join(parts, "")
	})

	rapid.Check(t, func(t *rapid.T) {
		name := cyrillicMixed.Draw(t, "name")
		fallback := nonEmptyFallback().Draw(t, "fallback")

		result := SanitizeComponent(name, fallback)

		// Extract Cyrillic runes from the input.
		for _, r := range name {
			if isCyrillic(r) {
				if !strings.ContainsRune(result, r) {
					t.Fatalf("Cyrillic character %q from input %q not preserved in output %q", string(r), name, result)
				}
			}
		}
	})
}

func TestProperty30_OutputAlwaysNonEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.String().Draw(t, "name")
		fallback := nonEmptyFallback().Draw(t, "fallback")

		result := SanitizeComponent(name, fallback)

		if result == "" {
			t.Fatalf("output is empty for input %q with fallback %q", name, fallback)
		}
	})
}

func TestProperty30_FallbackDeterministic(t *testing.T) {
	// Generator for inputs that sanitize to empty. After the replacement step,
	// the result is trimmed of whitespace and dots. Control chars (including \t, \n, \r)
	// are replaced with '_' which survives trimming. Only space (0x20) and dot pass
	// through unchanged and are then trimmed. So only inputs made entirely of
	// spaces and dots produce an empty result.
	emptyAfterSanitize := rapid.Custom(func(t *rapid.T) string {
		trimmable := []rune{'.', ' '}
		length := rapid.IntRange(1, 20).Draw(t, "length")
		var b strings.Builder
		for i := 0; i < length; i++ {
			idx := rapid.IntRange(0, len(trimmable)-1).Draw(t, "charIdx")
			b.WriteRune(trimmable[idx])
		}
		return b.String()
	})

	rapid.Check(t, func(t *rapid.T) {
		name := emptyAfterSanitize.Draw(t, "name")
		fallback := nonEmptyFallback().Draw(t, "fallback")

		result1 := SanitizeComponent(name, fallback)
		result2 := SanitizeComponent(name, fallback)

		if result1 != result2 {
			t.Fatalf("non-deterministic fallback: %q vs %q for input %q", result1, result2, name)
		}
		// When input sanitizes to empty, the fallback should be returned.
		if result1 != fallback {
			t.Fatalf("expected fallback %q but got %q for input %q that sanitizes to empty", fallback, result1, name)
		}
	})
}

// isCyrillic returns true if the rune is in the Cyrillic Unicode block.
func isCyrillic(r rune) bool {
	return unicode.Is(unicode.Cyrillic, r)
}
