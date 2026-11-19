package filetype

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/h2non/filetype/matchers"
	"github.com/h2non/filetype/types"
)

// This file contains stability regression tests. It must never require changes
// in the production packages: concurrent and repeated identification have to
// keep returning the exact same result as a single sequential call, and
// registry changes must only affect calls made after the change took effect.
//
// Run with the race detector, e.g.:
//
//	go test -race -run 'TestStability' ./...

// stabilityWorkers is the number of goroutines hammering the API concurrently.
const stabilityWorkers = 64

// stabilityIterations is how many times every goroutine repeats each check.
const stabilityIterations = 25

// stabilityRun hands out unique suffixes so registry mutations in repeated
// test runs (go test -count=N keeps one process) never alias each other.
var stabilityRun uint64

// loadStabilityFixtures reads every bundled sample file once and pairs the
// bytes with the extension the single-shot matcher is expected to return.
func loadStabilityFixtures(t *testing.T) []struct {
	path string
	ext  string
	buf  []byte
} {
	t.Helper()

	files := []struct {
		name string
		ext  string
	}{
		{"sample.avif", "avif"},
		{"sample.docx", "docx"},
		{"sample.dwg", "dwg"},
		{"sample.exr", "exr"},
		{"sample.gif", "gif"},
		{"sample.jpg", "jpg"},
		{"sample.mkv", "mkv"},
		{"sample.mov", "mov"},
		{"sample.mp4", "mp4"},
		{"sample.odp", "odp"},
		{"sample.ods", "ods"},
		{"sample.odt", "odt"},
		{"sample.parquet", "parquet"},
		{"sample.png", "png"},
		{"sample.pptx", "pptx"},
		{"sample.tar", "tar"},
		{"sample.tif", "tif"},
		{"sample.wasm", "wasm"},
		{"sample.webm", "webm"},
		{"sample.xlsx", "xlsx"},
		{"sample.zip", "zip"},
		{"sample.zst", "zst"},
		{"sample_skippable.zst", "zst"},
	}

	fixtures := make([]struct {
		path string
		ext  string
		buf  []byte
	}, 0, len(files))

	for _, f := range files {
		path := filepath.Join("fixtures", f.name)
		buf, err := ioutil.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read fixture %s: %v", path, err)
		}
		fixtures = append(fixtures, struct {
			path string
			ext  string
			buf  []byte
		}{path, f.ext, buf})
	}

	return fixtures
}

// TestStabilityConcurrentFixtures establishes a single-shot baseline for every
// bundled format and then runs the full identification API from many
// goroutines. Every concurrent result, including errors, must be identical to
// the baseline. This is the mandatory concurrency regression: detection for a
// given buffer must never drift between invocations.
func TestStabilityConcurrentFixtures(t *testing.T) {
	fixtures := loadStabilityFixtures(t)

	type expectation struct {
		kind types.Type
		err  error
	}
	baseline := make([]expectation, len(fixtures))
	for i, f := range fixtures {
		kind, err := Match(f.buf)
		if kind.Extension != f.ext {
			t.Fatalf("baseline mismatch for %s: got %q want %q", f.path, kind.Extension, f.ext)
		}
		baseline[i] = expectation{kind, err}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, stabilityWorkers)
	start := make(chan struct{})

	for w := 0; w < stabilityWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < stabilityIterations; n++ {
				for i, f := range fixtures {
					kind, err := Match(f.buf)
					want := baseline[i]
					if kind != want.kind || (err != nil) != (want.err != nil) {
						errCh <- fmt.Errorf("concurrent Match drifted for %s: got %q want %q",
							f.path, kind.Extension, want.kind.Extension)
						return
					}

					// Matches is the boolean wrapper on top of Match.
					if got := Matches(f.buf); got != (want.kind != types.Unknown) {
						errCh <- fmt.Errorf("concurrent Matches drifted for %s: got %v want %v",
							f.path, got, want.kind != types.Unknown)
						return
					}
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// syntheticBuffers returns a buffer whose magic signature satisfies the
// matcher registered for every single built-in type. It covers formats that
// ship without a sample file so the "all formats" guarantee is not limited to
// the fixtures directory. Buffers are padded generously because several
// matchers inspect fixed offsets well past the leading signature.
func syntheticBuffers() map[types.Type][]byte {
	const size = 512
	mk := func(setup func(b []byte)) []byte {
		b := make([]byte, size)
		setup(b)
		return b
	}
	mkLarge := func(size int, setup func(b []byte)) []byte {
		b := make([]byte, size)
		setup(b)
		return b
	}
	put := func(b []byte, off int, v ...byte) {
		copy(b[off:], v)
	}

	// A handful of offsets are reused by several magic signatures.
	return map[types.Type][]byte{
		// Image
		matchers.TypeJpeg: mk(func(b []byte) { put(b, 0, 0xFF, 0xD8, 0xFF) }),
		matchers.TypeJpeg2000: mk(func(b []byte) {
			put(b, 0, 0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A, 0x00)
		}),
		matchers.TypePng:  mk(func(b []byte) { put(b, 0, 0x89, 0x50, 0x4E, 0x47) }),
		matchers.TypeGif:  mk(func(b []byte) { put(b, 0, 0x47, 0x49, 0x46) }),
		matchers.TypeWebp: mk(func(b []byte) { put(b, 0, 0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50) }),
		matchers.TypeCR2:  mk(func(b []byte) { put(b, 0, 0x49, 0x49, 0x2A, 0x00, 0x00, 0x00, 0x00, 0x00, 0x43, 0x52, 0x02) }),
		matchers.TypeTiff: mk(func(b []byte) { put(b, 0, 0x49, 0x49, 0x2A, 0x00) }),
		matchers.TypeBmp:  mk(func(b []byte) { put(b, 0, 0x42, 0x4D) }),
		matchers.TypeJxr:  mk(func(b []byte) { put(b, 0, 0x49, 0x49, 0xBC) }),
		matchers.TypePsd:  mk(func(b []byte) { put(b, 0, 0x38, 0x42, 0x50, 0x53) }),
		matchers.TypeIco:  mk(func(b []byte) { put(b, 0, 0x00, 0x00, 0x01, 0x00) }),
		matchers.TypeHeif: mk(func(b []byte) { put(b, 4, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c') }),
		matchers.TypeDwg:  mk(func(b []byte) { put(b, 0, 'A', 'C', '1', '0') }),
		matchers.TypeExr:  mk(func(b []byte) { put(b, 0, 0x76, 0x2F, 0x31, 0x01) }),
		matchers.TypeAvif: mk(func(b []byte) { put(b, 4, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f') }),

		// Audio
		matchers.TypeMidi: mk(func(b []byte) { put(b, 0, 'M', 'T', 'h', 'd') }),
		matchers.TypeMp3:  mk(func(b []byte) { put(b, 0, 'I', 'D', '3') }),
		matchers.TypeM4a:  mk(func(b []byte) { put(b, 4, 'f', 't', 'y', 'p', 'M', '4', 'A') }),
		matchers.TypeOgg:  mk(func(b []byte) { put(b, 0, 'O', 'g', 'g', 'S') }),
		matchers.TypeFlac: mk(func(b []byte) { put(b, 0, 'f', 'L', 'a', 'C') }),
		matchers.TypeWav:  mk(func(b []byte) { put(b, 0, 'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E') }),
		matchers.TypeAmr:  mk(func(b []byte) { put(b, 0, '#', '!', 'A', 'M', 'R', '\n') }),
		matchers.TypeAac:  mk(func(b []byte) { put(b, 0, 0xFF, 0xF1) }),
		matchers.TypeAiff: mk(func(b []byte) { put(b, 0, 'F', 'O', 'R', 'M', 0, 0, 0, 0, 'A', 'I', 'F', 'F') }),

		// Video
		matchers.TypeMp4: mk(func(b []byte) { put(b, 4, 'f', 't', 'y', 'p', 'a', 'v', 'c', '1') }),
		matchers.TypeM4v: mk(func(b []byte) { put(b, 4, 'f', 't', 'y', 'p', 'M', '4', 'V') }),
		// Element ID 0x4282 must land at index-3/index-2 of the subtype
		// string, so the tag is written at offsets 11-12, offset 13 stays
		// empty and the subtype string starts at offset 14.
		matchers.TypeMkv: mk(func(b []byte) {
			put(b, 0, 0x1A, 0x45, 0xDF, 0xA3)
			put(b, 11, 0x42, 0x82, 0x00, 'm', 'a', 't', 'r', 'o', 's', 'k', 'a')
		}),
		matchers.TypeWebm: mk(func(b []byte) { put(b, 0, 0x1A, 0x45, 0xDF, 0xA3); put(b, 11, 0x42, 0x82, 0x00, 'w', 'e', 'b', 'm') }),
		matchers.TypeMov:  mk(func(b []byte) { put(b, 0, 0x00, 0x00, 0x00, 0x14, 'f', 't', 'y', 'p', 'q', 't') }),
		matchers.TypeAvi:  mk(func(b []byte) { put(b, 0, 'R', 'I', 'F', 'F', 0, 0, 0, 0, 'A', 'V', 'I') }),
		matchers.TypeWmv:  mk(func(b []byte) { put(b, 0, 0x30, 0x26, 0xB2, 0x75, 0x8E, 0x66, 0xCF, 0x11, 0xA6, 0xD9) }),
		matchers.TypeMpeg: mk(func(b []byte) { put(b, 0, 0x00, 0x00, 0x01, 0xBA) }),
		matchers.TypeFlv:  mk(func(b []byte) { put(b, 0, 'F', 'L', 'V', 0x01) }),
		matchers.Type3gp:  mk(func(b []byte) { put(b, 4, 'f', 't', 'y', 'p', '3', 'g', 'p') }),

		// Font
		matchers.TypeWoff:  mk(func(b []byte) { put(b, 0, 'w', 'O', 'F', 'F', 0x00, 0x01, 0x00, 0x00) }),
		matchers.TypeWoff2: mk(func(b []byte) { put(b, 0, 'w', 'O', 'F', '2', 0x00, 0x01, 0x00, 0x00) }),
		matchers.TypeTtf:   mk(func(b []byte) { put(b, 0, 0x00, 0x01, 0x00, 0x00, 0x00) }),
		matchers.TypeOtf:   mk(func(b []byte) { put(b, 0, 'O', 'T', 'T', 'O', 0x00) }),

		// Archive / generic binary containers
		matchers.TypeEpub: mk(func(b []byte) {
			put(b, 0, 'P', 'K', 0x03, 0x04, 'm', 'i', 'm', 'e', 't', 'y', 'p', 'e', 'a', 'p', 'p', 'l', 'i', 'c', 'a', 't', 'i', 'o', 'n', '/', 'e', 'p', 'u', 'b', '+', 'z', 'i', 'p')
		}),
		matchers.TypeZip:    mk(func(b []byte) { put(b, 0, 'P', 'K', 0x03, 0x04) }),
		matchers.TypeTar:    mk(func(b []byte) { put(b, 257, 'u', 's', 't', 'a', 'r') }),
		matchers.TypeRar:    mk(func(b []byte) { put(b, 0, 'R', 'a', 'r', '!', 0x1A, 0x07, 0x00) }),
		matchers.TypeGz:     mk(func(b []byte) { put(b, 0, 0x1F, 0x8B, 0x08) }),
		matchers.TypeBz2:    mk(func(b []byte) { put(b, 0, 'B', 'Z', 'h') }),
		matchers.Type7z:     mk(func(b []byte) { put(b, 0, '7', 'z', 0xBC, 0xAF, 0x27, 0x1C) }),
		matchers.TypeXz:     mk(func(b []byte) { put(b, 0, 0xFD, '7', 'z', 'X', 'Z', 0x00) }),
		matchers.TypeZstd:   mk(func(b []byte) { put(b, 0, 0x28, 0xB5, 0x2F, 0xFD) }),
		matchers.TypePdf:    mk(func(b []byte) { put(b, 0, '%', 'P', 'D', 'F') }),
		matchers.TypeExe:    mk(func(b []byte) { put(b, 0, 'M', 'Z') }),
		matchers.TypeSwf:    mk(func(b []byte) { put(b, 0, 'C', 'W', 'S') }),
		matchers.TypeRtf:    mk(func(b []byte) { put(b, 0, '{', '\\', 'r', 't', 'f') }),
		matchers.TypeEot:    mk(func(b []byte) { put(b, 8, 0x02, 0x00, 0x01); put(b, 34, 'L', 'P') }),
		matchers.TypePs:     mk(func(b []byte) { put(b, 0, '%', '!') }),
		matchers.TypeSqlite: mk(func(b []byte) { put(b, 0, 'S', 'Q', 'L', 'i') }),
		matchers.TypeNes:    mk(func(b []byte) { put(b, 0, 'N', 'E', 'S', 0x1A) }),
		matchers.TypeCrx:    mk(func(b []byte) { put(b, 0, 'C', 'r', '2', '4') }),
		matchers.TypeCab:    mk(func(b []byte) { put(b, 0, 'M', 'S', 'C', 'F') }),
		matchers.TypeDeb: mk(func(b []byte) {
			put(b, 0, '!', '<', 'a', 'r', 'c', 'h', '>', '\n', 'd', 'e', 'b', 'i', 'a', 'n', '-', 'b', 'i', 'n', 'a', 'r', 'y')
		}),
		matchers.TypeAr:  mk(func(b []byte) { put(b, 0, '!', '<', 'a', 'r', 'c', 'h', '>') }),
		matchers.TypeZ:   mk(func(b []byte) { put(b, 0, 0x1F, 0xA0) }),
		matchers.TypeLz:  mk(func(b []byte) { put(b, 0, 'L', 'Z', 'I', 'P') }),
		matchers.TypeRpm: mk(func(b []byte) { put(b, 0, 0xED, 0xAB, 0xEE, 0xDB) }),
		matchers.TypeElf: mk(func(b []byte) { put(b, 0, 0x7F, 'E', 'L', 'F') }),
		matchers.TypeDcm: mk(func(b []byte) { put(b, 128, 'D', 'I', 'C', 'M') }),
		// ISO 9660 magic lives at offset 32769, so it needs an oversized buffer.
		matchers.TypeIso:     mkLarge(32774, func(b []byte) { put(b, 32769, 'C', 'D', '0', '0', '1') }),
		matchers.TypeMachO:   mk(func(b []byte) { put(b, 0, 0xFE, 0xED, 0xFA, 0xCF) }),
		matchers.TypeParquet: mk(func(b []byte) { put(b, 0, 'P', 'A', 'R', '1') }),

		// Document
		matchers.TypeDoc:  mk(func(b []byte) { put(b, 0, 0xD0, 0xCF, 0x11, 0xE0) }),
		matchers.TypeDocx: mk(func(b []byte) { docxLike(b, "word/") }),
		matchers.TypeXls:  mkLarge(600, func(b []byte) { put(b, 0, 0xD0, 0xCF, 0x11, 0xE0); put(b, 512, 0x09, 0x08) }),
		matchers.TypeXlsx: mk(func(b []byte) { docxLike(b, "xl/") }),
		matchers.TypePpt:  mk(func(b []byte) { put(b, 0, 0xD0, 0xCF, 0x11, 0xE0); put(b, 512, 0xA0, 0x46) }),
		matchers.TypePptx: mk(func(b []byte) { docxLike(b, "ppt/") }),
		matchers.TypeOdp:  mk(func(b []byte) { odfLike(b, matchers.TypeOdp.MIME.Value) }),
		matchers.TypeOds:  mk(func(b []byte) { odfLike(b, matchers.TypeOds.MIME.Value) }),
		matchers.TypeOdt:  mk(func(b []byte) { odfLike(b, matchers.TypeOdt.MIME.Value) }),

		// Application
		matchers.TypeWasm: mk(func(b []byte) { put(b, 0, 0x00, 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00) }),
		matchers.TypeDex:  mk(func(b []byte) { put(b, 0, 'd', 'e', 'x', '\n'); b[36] = 'p' }),
		matchers.TypeDey: mk(func(b []byte) {
			put(b, 0, 'd', 'e', 'y', '\n')
			put(b, 40, 'd', 'e', 'x', '\n')
			b[76] = 'p' // offset 36 inside the embedded dex (40 + 36)
		}),
	}
}

// docxLike lays down a minimal OOXML local file header whose first entry name
// selects the desired office subtype.
func docxLike(b []byte, entry string) {
	copy(b[0:4], []byte{'P', 'K', 0x03, 0x04})
	// First entry starts at 0x1E; msooxml() inspects its name directly.
	copy(b[0x1E:], entry)
}

// odfLike lays down the minimal uncompressed "mimetype" first ZIP entry that
// the OpenDocument matchers require.
func odfLike(b []byte, mime string) {
	copy(b[0:4], []byte{'P', 'K', 0x03, 0x04})
	// Stored (uncompressed) entry.
	b[8], b[9] = 0x00, 0x00
	// Filename length: 8 ("mimetype"), little endian uint16.
	b[26], b[27] = 0x08, 0x00
	// Sizes equal the mimetype length.
	n := byte(len(mime))
	b[18], b[19], b[20], b[21] = n, 0x00, 0x00, 0x00
	b[22], b[23], b[24], b[25] = n, 0x00, 0x00, 0x00
	// No extra field.
	b[28], b[29] = 0x00, 0x00
	copy(b[30:38], "mimetype")
	copy(b[38:], mime)
}

// builtInTypeCount is the number of formats registered by the matchers
// package's init() function. Other tests may add their own entries to the
// process-global registry, so stability assertions must not assume the
// registry size stays exactly at this value; they instead require every
// built-in type to remain present and stable.
const builtInTypeCount = 79

// TestStabilityConcurrentAllFormats verifies the stronger "all formats"
// guarantee: for every built-in type a buffer that satisfies its matcher is
// identified identically by sequential and concurrent callers. The per-type
// IsType assertion isolates each matcher, while the Match assertion pins the
// whole ordered matcher pipeline. Entries registered later by other tests are
// intentionally out of scope.
func TestStabilityConcurrentAllFormats(t *testing.T) {
	buffers := syntheticBuffers()
	if len(buffers) != builtInTypeCount {
		t.Fatalf("synthetic buffer table covers %d built-in types, want %d",
			len(buffers), builtInTypeCount)
	}
	for kind := range buffers {
		if matchers.Matchers[kind] == nil {
			t.Fatalf("built-in type %q missing from matcher registry", kind.Extension)
		}
	}
	if len(matchers.Matchers) < builtInTypeCount {
		t.Fatalf("registry shrunk to %d entries, built-in set has %d",
			len(matchers.Matchers), builtInTypeCount)
	}

	type baseline struct {
		matchKind types.Type
		isType    bool
	}
	want := make(map[types.Type]baseline, len(buffers))
	for kind, buf := range buffers {
		if !IsType(buf, kind) {
			t.Fatalf("synthetic buffer for %q does not satisfy its own matcher", kind.Extension)
		}
		matched, err := Match(buf)
		if err != nil {
			t.Fatalf("baseline Match failed for %q: %v", kind.Extension, err)
		}
		want[kind] = baseline{matchKind: matched, isType: true}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, stabilityWorkers)
	start := make(chan struct{})

	for w := 0; w < stabilityWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < stabilityIterations; n++ {
				for kind, buf := range buffers {
					base := want[kind]

					if got := IsType(buf, kind); got != base.isType {
						errCh <- fmt.Errorf("concurrent IsType drifted for %s: got %v want %v",
							kind.Extension, got, base.isType)
						return
					}

					matched, err := Match(buf)
					if err != nil || matched != base.matchKind {
						errCh <- fmt.Errorf("concurrent Match drifted for synthetic %s: got %q want %q",
							kind.Extension, matched.Extension, base.matchKind.Extension)
						return
					}
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// TestStabilityRepeatedCalls checks plain repetition without concurrency:
// thousands of sequential calls must return exactly the same type and error
// as the first call, for every fixture and for both unknown and empty inputs.
func TestStabilityRepeatedCalls(t *testing.T) {
	fixtures := loadStabilityFixtures(t)

	for _, f := range fixtures {
		first, errFirst := Match(f.buf)
		for i := 0; i < 200; i++ {
			got, err := Match(f.buf)
			if got != first || (err != nil) != (errFirst != nil) {
				t.Fatalf("repeated Match drifted for %s at iteration %d: got %q want %q",
					f.path, i, got.Extension, first.Extension)
			}
		}
	}

	unknown := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}
	first, errFirst := Match(unknown)
	if first != types.Unknown || errFirst != nil {
		t.Fatalf("unknown buffer baseline wrong: kind=%q err=%v", first.Extension, errFirst)
	}
	for i := 0; i < 200; i++ {
		got, err := Match(unknown)
		if got != types.Unknown || err != nil {
			t.Fatalf("repeated Match drifted for unknown buffer at iteration %d", i)
		}
	}

	for i := 0; i < 200; i++ {
		got, err := Match(nil)
		if got != types.Unknown || err != ErrEmptyBuffer {
			t.Fatalf("empty buffer result drifted at iteration %d: kind=%q err=%v", i, got.Extension, err)
		}
	}
}

// TestStabilityRegistryChangeAffectsOnlyLaterCalls pins the temporal
// semantics of the registry. The registry is only mutated from this single
// goroutine, and all in-flight identification happens between well-defined
// registration points:
//
//  1. Before registration, the custom signature is unknown.
//  2. After registration, later calls resolve to the custom type.
//  3. Re-registering the same extension with an updated MIME only changes
//     subsequent lookups; results observed before the update stay valid.
func TestStabilityRegistryChangeAffectsOnlyLaterCalls(t *testing.T) {
	// Both the extension and the two signature bytes must be unique per
	// invocation: NewMatcher() prepends matchers into the process-global
	// MatcherKeys slice and never removes them, so a signature reused by a
	// later run would already match a matcher registered by an earlier run.
	// The counter is process-global and only moves forward, so every
	// invocation - including go test -count=N iterations which do not reset
	// package variables - gets distinct values without relying on wall time.
	runID := atomic.AddUint64(&stabilityRun, 1)
	suffix := fmt.Sprintf("%d", runID)
	ext := "stability-regression-custom-" + suffix
	firstMIME := "application/x-stability-regression-custom-" + suffix + "-v1"
	secondMIME := "application/x-stability-regression-custom-" + suffix + "-v2"
	// Spread the monotonic id over two bytes for the 2-byte signature.
	customSig := []byte{byte(runID >> 8), byte(runID)}

	// Phase 1: the signature must be unknown before anything is registered.
	if kind, _ := Match(customSig); kind != types.Unknown {
		t.Fatalf("custom signature matched before registration: %q", kind.Extension)
	}
	if IsSupported(ext) {
		t.Fatalf("extension %q supported before registration", ext)
	}

	// Phase 2: register type + matcher; subsequent calls must observe it.
	customType := AddType(ext, firstMIME)
	AddMatcher(customType, func(buf []byte) bool {
		return len(buf) == 2 && buf[0] == customSig[0] && buf[1] == customSig[1]
	})

	if !IsSupported(ext) {
		t.Fatalf("extension %q not supported after registration", ext)
	}
	if kind, err := Match(customSig); err != nil || kind.Extension != ext {
		t.Fatalf("post-registration Match got %q (err=%v), want %q", kind.Extension, err, ext)
	}
	if !IsType(customSig, customType) {
		t.Fatalf("IsType rejected custom signature after matcher registration")
	}
	if !Is(customSig, ext) {
		t.Fatalf("Is rejected custom signature after matcher registration")
	}

	// Snapshot the post-registration behavior; it must remain stable under
	// repeated concurrent use while the registry is not being mutated.
	baseline, baselineErr := Match(customSig)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for w := 0; w < stabilityWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < stabilityIterations; n++ {
				got, err := Match(customSig)
				if got != baseline || (err != nil) != (baselineErr != nil) {
					t.Errorf("custom matcher result drifted under concurrency: got %q want %q",
						got.Extension, baseline.Extension)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()

	// Phase 3: overwrite the extension's type. Calls made earlier in the test
	// captured the old type; only subsequent lookups must see the new one.
	updatedType := AddType(ext, secondMIME)
	if updatedType == customType {
		t.Fatalf("re-registration expected to produce an updated type value")
	}
	if got := GetType(ext); got.MIME.Value != secondMIME {
		t.Fatalf("GetType after re-registration got MIME %q, want %q", got.MIME.Value, secondMIME)
	}
	if kind, _ := Match(customSig); kind.Extension != ext {
		t.Fatalf("custom signature stopped matching after type re-registration")
	}
	// The previously captured baseline value remains a valid historical
	// result and must not have been mutated in place.
	if baseline.MIME.Value != firstMIME {
		t.Fatalf("earlier result was retroactively rewritten: MIME now %q, want %q",
			baseline.MIME.Value, firstMIME)
	}
}
