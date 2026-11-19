package filetype

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/h2non/filetype/types"
)

// referenceMatch is the pre-optimization implementation: a sequential walk
// over MatcherKeys with a map lookup per kind.
func referenceMatch(buf []byte) types.Type {
	if len(buf) == 0 {
		return types.Unknown
	}
	for _, kind := range *MatcherKeys {
		match := Matchers[kind](buf)
		if match != types.Unknown && match.Extension != "" {
			return match
		}
	}
	return types.Unknown
}

// TestMatchOptimizationDifferential proves behavioral equivalence on real
// fixture files and their truncated prefixes.
func TestMatchOptimizationDifferential(t *testing.T) {
	files, err := filepath.Glob("./fixtures/sample*")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixture files found")
	}

	prefixes := []int{1, 2, 3, 4, 7, 8, 11, 12, 36, 37, 53, 97, 101, 132, 262, 514, 4096}
	for _, path := range files {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		candidates := [][]byte{data}
		for _, n := range prefixes {
			if n < len(data) {
				candidates = append(candidates, data[:n])
			}
		}

		for idx, buf := range candidates {
			want := referenceMatch(buf)
			got, gotErr := Match(buf)
			wantErr := len(buf) == 0
			if got != want || (gotErr != nil) != wantErr {
				t.Fatalf("%s candidate %d (len=%d): optimized=(%v,err=%v) reference=(%v,wantErr=%v)",
					filepath.Base(path), idx, len(buf), got.Extension, gotErr != nil, want.Extension, wantErr)
			}
		}
	}
}

// TestMatchOptimizationUnknownDifferential covers non-matching buffers,
// where every matcher has to be consulted.
func TestMatchOptimizationUnknownDifferential(t *testing.T) {
	cases := [][]byte{
		make([]byte, 8192),
		{0x00},
		{0xFF, 0x00, 0x00},
		{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11},
	}
	for i, buf := range cases {
		got, _ := Match(buf)
		if want := referenceMatch(buf); got != want {
			t.Fatalf("unknown case %d: optimized=%v reference=%v", i, got, want)
		}
	}
}

// BenchmarkReferenceMatch* keep the old map-lookup loop in the benchmark
// binary so the speedup is measurable side by side.

func BenchmarkReferenceMatchUnknown(b *testing.B) {
	buf := make([]byte, 8192)
	for n := 0; n < b.N; n++ {
		referenceMatch(buf)
	}
}

func BenchmarkMatchUnknown(b *testing.B) {
	buf := make([]byte, 8192)
	for n := 0; n < b.N; n++ {
		Match(buf)
	}
}

func BenchmarkReferenceMatchPng(b *testing.B) {
	for n := 0; n < b.N; n++ {
		referenceMatch(pngBuffer)
	}
}

func BenchmarkReferenceMatchJpeg(b *testing.B) {
	for n := 0; n < b.N; n++ {
		referenceMatch(jpgBuffer)
	}
}

func BenchmarkReferenceMatchTar(b *testing.B) {
	for n := 0; n < b.N; n++ {
		referenceMatch(tarBuffer)
	}
}

func BenchmarkReferenceMatchDocx(b *testing.B) {
	for n := 0; n < b.N; n++ {
		referenceMatch(docxBuffer)
	}
}
