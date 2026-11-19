package filetype

import (
	"bytes"
	"io/ioutil"
	"testing"

	"github.com/h2non/filetype/matchers"
	"github.com/h2non/filetype/types"
)

// codecFixtures maps fixture files to their expected container extension and
// codec. Every expectation was cross-checked with ffprobe.
var codecFixtures = []struct {
	file  string
	ext   string
	codec string
}{
	// Pre-existing fixtures
	{"sample.mp4", "mp4", "mpeg4"},
	{"sample.mkv", "mkv", "h264"},
	{"sample.webm", "webm", "vp9"},
	{"sample.mov", "mov", "svq1"},
	// Same container, different codecs
	{"codec_h264.mp4", "mp4", "h264"},
	{"codec_h264_faststart.mp4", "mp4", "h264"},
	{"codec_hevc.mp4", "mp4", "hevc"},
	{"codec_vp8.webm", "webm", "vp8"},
	{"codec_vp9.webm", "webm", "vp9"},
	{"codec_av1.webm", "webm", "av1"},
	{"codec_h264.mkv", "mkv", "h264"},
	{"codec_mpeg4.avi", "avi", "mpeg4"},
	{"codec_mjpeg.avi", "avi", "mjpeg"},
	{"codec_h264.flv", "flv", "h264"},
	{"codec_wmv2.wmv", "wmv", "wmv2"},
	{"codec_mpeg2.mpg", "mpg", "mpeg2"},
	{"codec_opus.ogg", "ogg", "opus"},
	{"codec_vorbis.ogg", "ogg", "vorbis"},
	{"codec_pcm.wav", "wav", "pcm"},
	{"codec_mp3.mp3", "mp3", "mp3"},
}

func TestMatchCodecFile(t *testing.T) {
	for _, test := range codecFixtures {
		kind, err := MatchCodecFile("./fixtures/" + test.file)
		if err != nil {
			t.Fatalf("%s: unexpected error: %s", test.file, err)
		}
		if kind.Extension != test.ext {
			t.Fatalf("%s: invalid container type: %s != %s", test.file, kind.Extension, test.ext)
		}
		if kind.Codec != test.codec {
			t.Fatalf("%s: invalid codec: %s != %s", test.file, kind.Codec, test.codec)
		}
	}
}

func TestMatchCodec(t *testing.T) {
	for _, test := range codecFixtures {
		buf, err := ioutil.ReadFile("./fixtures/" + test.file)
		if err != nil {
			t.Fatalf("%s: cannot read fixture: %s", test.file, err)
		}
		kind, err := MatchCodec(buf)
		if err != nil {
			t.Fatalf("%s: unexpected error: %s", test.file, err)
		}
		if kind.Extension != test.ext {
			t.Fatalf("%s: invalid container type: %s != %s", test.file, kind.Extension, test.ext)
		}
		if kind.Codec != test.codec {
			t.Fatalf("%s: invalid codec: %s != %s", test.file, kind.Codec, test.codec)
		}
	}
}

func TestMatchCodecReader(t *testing.T) {
	buf, err := ioutil.ReadFile("./fixtures/codec_h264_faststart.mp4")
	if err != nil {
		t.Fatalf("Cannot read fixture: %s", err)
	}
	kind, err := MatchCodecReader(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	if kind.Extension != "mp4" || kind.Codec != "h264" {
		t.Fatalf("Invalid type: %s codec: %s", kind.Extension, kind.Codec)
	}
}

// The codec-level result must always be consistent with the container-level
// Match result: identical extension and MIME, codec only adds depth.
func TestMatchCodecConsistentWithMatch(t *testing.T) {
	for _, test := range codecFixtures {
		buf, err := ioutil.ReadFile("./fixtures/" + test.file)
		if err != nil {
			t.Fatalf("%s: cannot read fixture: %s", test.file, err)
		}
		container, err := Match(buf)
		if err != nil {
			t.Fatalf("%s: unexpected error: %s", test.file, err)
		}
		kind, err := MatchCodec(buf)
		if err != nil {
			t.Fatalf("%s: unexpected error: %s", test.file, err)
		}
		if kind.Extension != container.Extension || kind.MIME != container.MIME {
			t.Fatalf("%s: codec-level result %v inconsistent with container-level %v", test.file, kind, container)
		}
		if kind.Codec == "" && kind != container {
			t.Fatalf("%s: fallback result differs from container-level result", test.file)
		}
	}
}

// When the codec cannot be determined, MatchCodec must fall back to the
// container-level result.
func TestMatchCodecFallback(t *testing.T) {
	// ftyp-only MP4 header: no moov box, no codec information
	mp4Header := []byte{
		0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x00, 0x00,
		'i', 's', 'o', 'm', 'm', 'p', '4', '2',
	}
	kind, err := MatchCodec(mp4Header)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	if kind.Extension != "mp4" {
		t.Fatalf("Invalid container type: %s", kind.Extension)
	}
	if kind.Codec != "" {
		t.Fatalf("Expected empty codec, got: %s", kind.Codec)
	}
	container, _ := Match(mp4Header)
	if kind != container {
		t.Fatalf("Fallback result differs from container-level result")
	}

	// Non audio/video types never get a codec
	png := []byte{0x89, 0x50, 0x4E, 0x47}
	kind, _ = MatchCodec(png)
	if kind.Extension != "png" || kind.Codec != "" {
		t.Fatalf("Unexpected codec result for png: %v", kind)
	}

	// Unknown buffers stay unknown
	kind, _ = MatchCodec([]byte{0x00, 0x01, 0x02})
	if kind != types.Unknown {
		t.Fatalf("Expected unknown type, got: %v", kind)
	}
}

// ftyp brand based detection: an avc1 major brand implies H.264 even
// without a sample description.
func TestMatchCodecFtypBrand(t *testing.T) {
	buf := []byte{
		0x00, 0x00, 0x00, 0x14, 'f', 't', 'y', 'p',
		'a', 'v', 'c', '1', 0x00, 0x00, 0x00, 0x00,
		'a', 'v', 'c', '1', 'i', 's', 'o', 'm',
	}
	kind, err := MatchCodec(buf)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	if kind.Extension != "mp4" || kind.Codec != "h264" {
		t.Fatalf("Invalid type: %s codec: %s", kind.Extension, kind.Codec)
	}
}

func TestIsCodec(t *testing.T) {
	buf, err := ioutil.ReadFile("./fixtures/codec_vp9.webm")
	if err != nil {
		t.Fatalf("Cannot read fixture: %s", err)
	}
	if !IsCodec(buf, "vp9") {
		t.Fatalf("Expected vp9 codec match")
	}
	if IsCodec(buf, "vp8") {
		t.Fatalf("Unexpected vp8 codec match")
	}
}

// Match must remain purely container-level: the Codec field stays empty.
func TestMatchLeavesCodecEmpty(t *testing.T) {
	buf, err := ioutil.ReadFile("./fixtures/codec_h264.mp4")
	if err != nil {
		t.Fatalf("Cannot read fixture: %s", err)
	}
	kind, err := Match(buf)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}
	if kind.Extension != "mp4" || kind.Codec != "" {
		t.Fatalf("Match result changed: %v", kind)
	}
}

func TestDetectCodecsMultiple(t *testing.T) {
	buf, err := ioutil.ReadFile("./fixtures/codec_h264.mkv")
	if err != nil {
		t.Fatalf("Cannot read fixture: %s", err)
	}
	codecs := matchers.DetectCodecs(matchers.TypeMkv, buf)
	if len(codecs) != 2 || codecs[0] != "h264" || codecs[1] != "aac" {
		t.Fatalf("Invalid codecs: %v", codecs)
	}
}

// Codec detection must never panic on truncated or corrupted input.
func TestMatchCodecTruncated(t *testing.T) {
	for _, test := range codecFixtures {
		buf, err := ioutil.ReadFile("./fixtures/" + test.file)
		if err != nil {
			t.Fatalf("%s: cannot read fixture: %s", test.file, err)
		}
		for n := 0; n <= len(buf); n += 97 {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s truncated to %d bytes: panic: %v", test.file, n, r)
					}
				}()
				MatchCodec(buf[:n])
			}()
		}
	}
}
