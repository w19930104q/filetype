package matchers

import (
	"github.com/h2non/filetype/types"
)

// Internal shortcut to NewType
var newType = types.NewType

// Matcher function interface as type alias
type Matcher func([]byte) bool

// Type interface to store pairs of type with its matcher function
type Map map[types.Type]Matcher

// Type specific matcher function interface
type TypeMatcher func([]byte) types.Type

// Store registered file type matchers
var Matchers = make(map[types.Type]TypeMatcher)
var MatcherKeys []types.Type

// compiledMatcher is a pre-resolved entry in the exact order of MatcherKeys.
// Keeping the matcher function next to its kind removes the per-call map
// lookup that the sequential Match loop otherwise performs for every kind.
type compiledMatcher struct {
	kind    types.Type
	matcher TypeMatcher
	// firstByteMask is non-nil only when the matcher can possibly return
	// true for a known, fixed set of leading bytes. A nil mask means the
	// matcher must always run (its logic does not constrain buf[0], or it
	// was registered externally through NewMatcher).
	firstByteMask *[256]bool
}

// compiledMatchers is the flattened, order-preserving view of MatcherKeys,
// rebuilt whenever a matcher is registered.
var compiledMatchers []compiledMatcher

// firstByteMasks records, for each built-in kind, the set of leading bytes
// for which the kind's matcher may possibly return true. Every entry is
// derived directly from the byte checks in the matcher source: the mask is
// always a superset of the real condition, so skipping can never exclude a
// buffer the original matcher would have matched. Kinds without an entry
// have no constraint on their first byte and are always evaluated.
var firstByteMasks = map[types.Type]*[256]bool{
	// archive
	TypeEpub:    mask(0x50),
	TypeZip:     mask(0x50),
	TypeRar:     mask(0x52),
	TypeGz:      mask(0x1F),
	TypeBz2:     mask(0x42),
	Type7z:      mask(0x37),
	TypeXz:      mask(0xFD),
	TypeZstd:    zstdFirstByteMask(), // zstd frame (0x28) or skippable frame (0x50..0x5F, little-endian)
	TypePdf:     mask(0x25),
	TypeExe:     mask(0x4D),
	TypeSwf:     mask(0x43, 0x46),
	TypeRtf:     mask(0x7B),
	TypePs:      mask(0x25),
	TypeSqlite:  mask(0x53),
	TypeNes:     mask(0x4E),
	TypeCrx:     mask(0x43),
	TypeCab:     mask(0x4D, 0x49),
	TypeDeb:     mask(0x21),
	TypeAr:      mask(0x21),
	TypeZ:       mask(0x1F),
	TypeLz:      mask(0x4C),
	TypeRpm:     mask(0xED),
	TypeElf:     mask(0x7F),
	TypeMachO:   mask(0xFE, 0xBE, 0xCF, 0xCE, 0xCA),
	TypeParquet: mask(0x50),
	// TypeTar, TypeEot, TypeDcm, TypeIso inspect fixed offsets other than
	// byte 0, so they are intentionally left without a mask.

	// document
	TypeDoc:  mask(0xD0),
	TypeXls:  mask(0xD0),
	TypePpt:  mask(0xD0),
	TypeDocx: mask(0x50),
	TypeXlsx: mask(0x50),
	TypePptx: mask(0x50),
	TypeOdp:  mask(0x50),
	TypeOds:  mask(0x50),
	TypeOdt:  mask(0x50),

	// image
	TypeJpeg:     mask(0xFF),
	TypeJpeg2000: mask(0x00),
	TypePng:      mask(0x89),
	TypeGif:      mask(0x47),
	TypeCR2:      mask(0x49, 0x4D),
	TypeTiff:     mask(0x49, 0x4D),
	TypeBmp:      mask(0x42),
	TypeJxr:      mask(0x49),
	TypePsd:      mask(0x38),
	TypeIco:      mask(0x00),
	TypeDwg:      mask(0x41),
	TypeExr:      mask(0x76),
	// TypeWebp, TypeHeif, TypeAvif check offset 4+ ("ftyp"/"WEBP"), no
	// constraint on byte 0, so they stay unmasked.

	// audio
	TypeMidi: mask(0x4D),
	TypeMp3:  mask(0x49, 0xFF),
	TypeOgg:  mask(0x4F),
	TypeFlac: mask(0x66),
	TypeWav:  mask(0x52),
	TypeAmr:  mask(0x23),
	TypeAac:  mask(0xFF),
	TypeAiff: mask(0x46),
	// TypeM4a accepts a branch starting at offset 4, so it stays unmasked.

	// video
	TypeMkv:  mask(0x1A),
	TypeWebm: mask(0x1A),
	TypeAvi:  mask(0x52),
	TypeWmv:  mask(0x30),
	TypeMpeg: mask(0x00),
	TypeFlv:  mask(0x46),
	// TypeMp4, TypeM4v, TypeMov and Type3gp branch on offset 4+, so they
	// stay unmasked.

	// font
	TypeWoff:  mask(0x77),
	TypeWoff2: mask(0x77),
	TypeTtf:   mask(0x00),
	TypeOtf:   mask(0x4F),

	// application
	TypeWasm: mask(0x00),
	TypeDex:  mask(0x64),
	TypeDey:  mask(0x64),
}

// mask builds a leading-byte lookup table from the given byte values.
func mask(bytes ...byte) *[256]bool {
	var m [256]bool
	for _, b := range bytes {
		m[b] = true
	}
	return &m
}

// zstdFirstByteMask covers both Zst() entry paths: the standard frame magic
// starts with 0x28, while a little-endian skippable-frame magic
// (0x184D2A5x) starts with any byte from 0x50 through 0x5F.
func zstdFirstByteMask() *[256]bool {
	m := mask(0x28)
	for b := 0x50; b <= 0x5F; b++ {
		m[b] = true
	}
	return m
}

// Create and register a new type matcher function
func NewMatcher(kind types.Type, fn Matcher) TypeMatcher {
	matcher := func(buf []byte) types.Type {
		if fn(buf) {
			return kind
		}
		return types.Unknown
	}

	Matchers[kind] = matcher
	// prepend here so any user defined matchers get added first
	MatcherKeys = append([]types.Type{kind}, MatcherKeys...)
	rebuildCompiledMatchers()
	return matcher
}

// rebuildCompiledMatchers flattens MatcherKeys into a slice that stores the
// resolved TypeMatcher and its optional first-byte guard next to the kind.
// Only built-in kinds listed in firstByteMasks get a guard; matchers added
// through the public API are always executed, preserving their precedence.
func rebuildCompiledMatchers() {
	compiled := make([]compiledMatcher, 0, len(MatcherKeys))
	for _, kind := range MatcherKeys {
		compiled = append(compiled, compiledMatcher{
			kind:          kind,
			matcher:       Matchers[kind],
			firstByteMask: firstByteMasks[kind],
		})
	}
	compiledMatchers = compiled
}

// MatchRegistered runs the registered matchers in MatcherKeys order against
// buf and returns the first matching kind, or types.Unknown. It is
// behaviorally identical to iterating MatcherKeys/Matchers directly, but
// resolves matcher functions once at registration time and skips, via the
// first-byte guards, matchers that could not possibly match buf[0].
func MatchRegistered(buf []byte) types.Type {
	if len(buf) == 0 {
		return types.Unknown
	}
	first := buf[0]
	for i := range compiledMatchers {
		entry := &compiledMatchers[i]
		if entry.firstByteMask != nil && !entry.firstByteMask[first] {
			continue
		}
		if match := entry.matcher(buf); match != types.Unknown && match.Extension != "" {
			return match
		}
	}
	return types.Unknown
}

func register(matchers ...Map) {
	MatcherKeys = MatcherKeys[:0]
	for _, m := range matchers {
		for kind, matcher := range m {
			NewMatcher(kind, matcher)
		}
	}
}

func init() {
	// Arguments order is intentional
	// Archive files will be checked last due to prepend above in func NewMatcher
	register(Archive, Document, Font, Audio, Video, Image, Application)
}
