package matchers

import (
	"math/rand"
	"testing"

	"github.com/h2non/filetype/types"
)

// referenceMatch reproduces the pre-optimization matching loop exactly:
// iterate MatcherKeys in order, resolve the matcher from the Matchers map
// and return the first kind whose matcher returns a non-empty result.
func referenceMatch(buf []byte) types.Type {
	if len(buf) == 0 {
		return types.Unknown
	}
	for _, kind := range MatcherKeys {
		match := Matchers[kind](buf)
		if match != types.Unknown && match.Extension != "" {
			return match
		}
	}
	return types.Unknown
}

// syntheticBuffers builds in-memory buffers without depending on fixture
// files (which live in the repository's root package): short prefixes,
// degenerate buffers, crafted magic prefixes and randomized data covering
// every possible leading byte.
func syntheticBuffers(rng *rand.Rand) [][]byte {
	var bufs [][]byte

	magicPrefixes := [][]byte{
		{0xFF, 0xD8, 0xFF},
		{0x89, 0x50, 0x4E, 0x47},
		{0x47, 0x49, 0x46},
		{0x50, 0x4B, 0x03, 0x04},
		{0x50, 0x4B, 0x05, 0x06},
		{0x28, 0xB5, 0x2F, 0xFD},
		{0x50, 0x2A, 0x4D, 0x18}, // zstd skippable frame magic (little-endian)
		{0x1F, 0x8B, 0x08},
		{0x42, 0x5A, 0x68},
		{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C},
		{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00},
		{0x25, 0x50, 0x44, 0x46},
		{0x4D, 0x5A},
		{0x7F, 0x45, 0x4C, 0x46},
		{0x7B, 0x5C, 0x72, 0x74, 0x66},
		{0x53, 0x51, 0x4C, 0x69},
		{0x4E, 0x45, 0x53, 0x1A},
		{0x43, 0x72, 0x32, 0x34},
		{0x21, 0x3C, 0x61, 0x72, 0x63, 0x68, 0x3E},
		{0x1F, 0xA0},
		{0x4C, 0x5A, 0x49, 0x50},
		{0xED, 0xAB, 0xEE, 0xDB},
		{0x50, 0x41, 0x52, 0x31},
		{0x49, 0x49, 0x2A, 0x00},
		{0x4D, 0x4D, 0x00, 0x2A},
		{0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20},
		{0x38, 0x42, 0x50, 0x53},
		{0x41, 0x43, 0x31, 0x30},
		{0x76, 0x2F, 0x31, 0x01},
		{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00},
		{0x64, 0x65, 0x78, 0x0A},
		{0x64, 0x65, 0x79, 0x0A},
		{0x1A, 0x45, 0xDF, 0xA3},
		{0x00, 0x00, 0x01, 0xB0},
		{0x46, 0x4C, 0x56, 0x01},
		{0x77, 0x4F, 0x46, 0x46},
		{0x77, 0x4F, 0x46, 0x32},
		{0x00, 0x01, 0x00, 0x00, 0x00},
		{0x4F, 0x54, 0x54, 0x4F, 0x00},
		{0x4D, 0x54, 0x68, 0x64},
		{0x49, 0x44, 0x33},
		{0x4F, 0x67, 0x67, 0x53},
		{0x66, 0x4C, 0x61, 0x43},
		{0x52, 0x49, 0x46, 0x46},
		{0x23, 0x21, 0x41, 0x4D, 0x52, 0x0A},
		{0xFF, 0xF1},
		{0x46, 0x4F, 0x52, 0x4D},
		{0x52, 0x49, 0x46, 0x46},
		{0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11, 0xA6, 0xD9},
		{0x43, 0x57, 0x53},
		{0x46, 0x57, 0x53},
		{0xD0, 0xCF, 0x11, 0xE0},
		{0x4D, 0x53, 0x43, 0x46},
		{0x49, 0x53, 0x63, 0x28},
	}

	// Prefixes themselves, plus padded variants long enough to pass length
	// guards and, where relevant, offset-based checks.
	for _, prefix := range magicPrefixes {
		bufs = append(bufs, prefix)
		for _, n := range []int{8, 16, 64, 262, 514, 8192} {
			if n > len(prefix) {
				buf := make([]byte, n)
				copy(buf, prefix)
				bufs = append(bufs, buf)
			}
		}
	}

	// Degenerate buffers.
	bufs = append(bufs, []byte{}, make([]byte, 1), make([]byte, 8192))

	// One buffer for every possible leading byte, plus fully random and
	// low-entropy buffers of varied lengths.
	for b := 0; b < 256; b++ {
		buf := make([]byte, 64)
		rng.Read(buf[1:])
		buf[0] = byte(b)
		bufs = append(bufs, buf)
	}
	for _, n := range []int{2, 5, 16, 256, 4096, 8192} {
		buf := make([]byte, n)
		rng.Read(buf)
		bufs = append(bufs, buf)

		sparse := make([]byte, n)
		for j := range sparse {
			if rng.Intn(16) == 0 {
				sparse[j] = byte(rng.Intn(256))
			}
		}
		bufs = append(bufs, sparse)
	}

	return bufs
}

// TestCompiledOrderParity guarantees the compiled matcher slice is a
// faithful, order-preserving projection of the public registry, so the
// optimized path cannot change precedence between overlapping matchers.
func TestCompiledOrderParity(t *testing.T) {
	if len(compiledMatchers) != len(MatcherKeys) {
		t.Fatalf("compiled length %d != MatcherKeys length %d",
			len(compiledMatchers), len(MatcherKeys))
	}
	for i, kind := range MatcherKeys {
		entry := compiledMatchers[i]
		if entry.kind != kind {
			t.Fatalf("compiled[%d] kind mismatch: %v != %v", i, entry.kind, kind)
		}
		if entry.matcher == nil {
			t.Fatalf("compiled[%d] has nil matcher for %v", i, kind)
		}
		if entry.firstByteMask != firstByteMasks[kind] {
			t.Fatalf("compiled[%d] mask mismatch for %v", i, kind)
		}
	}
}

// TestFirstByteMasksAreSupersets verifies the soundness of every guard:
// whenever a masked matcher actually matches a buffer, its leading byte
// must be present in the mask. Masks must never exclude a true match.
func TestFirstByteMasksAreSupersets(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for kind, m := range firstByteMasks {
		matcher := Matchers[kind]
		if matcher == nil {
			t.Fatalf("no registered matcher for masked kind %v", kind)
		}
		for _, buf := range syntheticBuffers(rng) {
			if len(buf) == 0 {
				continue
			}
			if matcher(buf) != types.Unknown && !m[buf[0]] {
				t.Fatalf("mask for %v excludes byte 0x%02X on a buffer the matcher accepts",
					kind, buf[0])
			}
		}
	}
}

// TestMatchRegisteredDifferential compares the optimized path against the
// reference implementation across all synthetic edge-case buffers.
func TestMatchRegisteredDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i, buf := range syntheticBuffers(rng) {
		want := referenceMatch(buf)
		got := MatchRegistered(buf)
		if got != want {
			first := "-"
			if len(buf) > 0 {
				first = string(rune(buf[0]))
			}
			t.Fatalf("case %d (len=%d, first=%q): optimized=%q reference=%q",
				i, len(buf), first, got.Extension, want.Extension)
		}
	}
}

// TestUserMatcherStaysUnmasked ensures matchers registered through the
// public API are always evaluated and keep their prepended precedence,
// regardless of the buffer's leading byte.
func TestUserMatcherStaysUnmasked(t *testing.T) {
	custom := newType("test-custom", "application/x-test-custom")
	NewMatcher(custom, func(buf []byte) bool {
		return len(buf) >= 2 && buf[0] == 0x99 && buf[1] == 0x77
	})

	entry := compiledMatchers[0]
	if entry.kind != custom {
		t.Fatalf("user matcher not prepended: got %v", entry.kind)
	}
	if entry.firstByteMask != nil {
		t.Fatalf("user matcher must run unmasked, got guard %v", entry.firstByteMask)
	}

	buf := []byte{0x99, 0x77, 0x00}
	if got := MatchRegistered(buf); got != custom {
		t.Fatalf("user matcher not reached: got %v", got.Extension)
	}
	if got := referenceMatch(buf); got != custom {
		t.Fatalf("reference mismatch for user matcher: got %v", got.Extension)
	}

	other := []byte{0x89, 0x50, 0x4E, 0x47}
	if MatchRegistered(other) != referenceMatch(other) {
		t.Fatal("divergence with user matcher registered on unrelated buffer")
	}
}
