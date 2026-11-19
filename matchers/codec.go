package matchers

import (
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/h2non/filetype/types"
)

// DetectCodec tries to identify the codec of the media streams stored inside
// an audio/video container matched at container level. It returns the primary
// codec name (video codecs take precedence over audio codecs), or an empty
// string when the codec cannot be determined, in which case callers are
// expected to fall back to the container-level result.
func DetectCodec(kind types.Type, buf []byte) string {
	codecs := DetectCodecs(kind, buf)
	if len(codecs) == 0 {
		return ""
	}
	return codecs[0]
}

// DetectCodecs returns every codec detected inside the given buffer, video
// codecs first, or nil when no codec can be determined.
func DetectCodecs(kind types.Type, buf []byte) []string {
	switch kind {
	case TypeMp4, TypeM4v, TypeM4a, TypeMov, Type3gp:
		return isoBmffCodecs(buf)
	case TypeMkv, TypeWebm:
		return ebmlCodecs(buf)
	case TypeAvi:
		return aviCodecs(buf)
	case TypeOgg:
		return oggCodecs(buf)
	case TypeWav:
		return wavCodecs(buf)
	case TypeFlv:
		return flvCodecs(buf)
	case TypeWmv:
		return asfCodecs(buf)
	case TypeMpeg:
		return mpegCodecs(buf)
	case TypeMp3:
		return []string{"mp3"}
	case TypeFlac:
		return []string{"flac"}
	case TypeAac:
		return []string{"aac"}
	case TypeAmr:
		return []string{"amr"}
	case TypeMidi:
		return []string{"midi"}
	case TypeAiff:
		return []string{"pcm"}
	}
	return nil
}

func appendUnique(list []string, codec string) []string {
	if codec == "" {
		return list
	}
	for _, c := range list {
		if c == codec {
			return list
		}
	}
	return append(list, codec)
}

func joinCodecs(videos, audios []string) []string {
	if len(videos) == 0 && len(audios) == 0 {
		return nil
	}
	return append(videos, audios...)
}

//
// ISO Base Media File Format (mp4, m4v, m4a, mov, 3gp)
//

// isoContainerBoxes are the boxes on the moov -> trak -> mdia -> minf ->
// stbl -> stsd path that are worth descending into.
var isoContainerBoxes = map[string]bool{
	"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true,
}

// isoSampleEntries maps ISO-BMFF sample entry fourccs to codec names.
// The video flag drives the video-first ordering of the results.
var isoSampleEntries = map[string]struct {
	codec string
	video bool
}{
	"avc1": {"h264", true}, "avc2": {"h264", true}, "avc3": {"h264", true}, "avc4": {"h264", true},
	"hvc1": {"hevc", true}, "hev1": {"hevc", true}, "dvhe": {"hevc", true}, "dvh1": {"hevc", true},
	"vp08": {"vp8", true}, "vp09": {"vp9", true}, "av01": {"av1", true},
	"mp4v": {"mpeg4", true}, "mp4s": {"mpeg4", true},
	"s263": {"h263", true}, "h263": {"h263", true},
	"mjpa": {"mjpeg", true}, "mjpb": {"mjpeg", true}, "jpeg": {"mjpeg", true},
	"SVQ1": {"svq1", true}, "SVQ2": {"svq2", true}, "SVQ3": {"svq3", true},
	"vc-1": {"vc1", true},
	"mp4a": {"aac", false},
	"ac-3": {"ac3", false}, "ec-3": {"eac3", false},
	"Opus": {"opus", false}, "fLaC": {"flac", false}, "alac": {"alac", false},
	"twos": {"pcm", false}, "sowt": {"pcm", false}, "lpcm": {"pcm", false},
	"in24": {"pcm", false}, "in32": {"pcm", false},
	"fl32": {"pcm", false}, "fl64": {"pcm", false}, "raw ": {"pcm", false},
	"samr": {"amr", false}, "sawb": {"amrwb", false},
	".mp3": {"mp3", false},
	"dtsc": {"dts", false}, "dtsh": {"dts", false}, "dtsl": {"dts", false}, "dtse": {"dts", false},
	"QDMC": {"qdmc", false}, "QDM2": {"qdmc", false},
}

func isoBmffCodecs(buf []byte) []string {
	var videos, audios []string

	walkIsoBoxes(buf, func(typ string, payload []byte) {
		if typ != "stsd" {
			return
		}
		v, a := parseStsd(payload)
		for _, c := range v {
			videos = appendUnique(videos, c)
		}
		for _, c := range a {
			audios = appendUnique(audios, c)
		}
	})

	codecs := joinCodecs(videos, audios)
	if len(codecs) > 0 {
		return codecs
	}

	// Fall back to the ftyp brands: "avc1" as major/compatible brand
	// implies an H.264 stream.
	if len(buf) >= 12 && string(buf[4:8]) == "ftyp" {
		ftypLength := int(binary.BigEndian.Uint32(buf[0:4]))
		if ftypLength > len(buf) {
			ftypLength = len(buf)
		}
		for i := 8; i+4 <= ftypLength; i += 4 {
			if string(buf[i:i+4]) == "avc1" {
				return []string{"h264"}
			}
		}
	}

	return nil
}

// walkIsoBoxes iterates over the ISO-BMFF boxes of buf, descending into the
// container boxes that lead to the sample description, and calls visit with
// the type and payload of every box found. Truncated trailing boxes (buffer
// cut in the middle of a box) are still visited with their partial payload.
func walkIsoBoxes(buf []byte, visit func(typ string, payload []byte)) {
	for off := 0; off+8 <= len(buf); {
		size := int(binary.BigEndian.Uint32(buf[off : off+4]))
		typ := string(buf[off+4 : off+8])
		hdr := 8
		switch size {
		case 0: // box extends to the end of the data
			size = len(buf) - off
		case 1: // 64-bit largesize
			if off+16 > len(buf) {
				return
			}
			large := binary.BigEndian.Uint64(buf[off+8 : off+16])
			if large > uint64(len(buf)) {
				return
			}
			size = int(large)
			hdr = 16
		}
		if size < hdr {
			return
		}
		end := off + size
		truncated := false
		if end > len(buf) {
			end = len(buf)
			truncated = true
		}
		payload := buf[off+hdr : end]
		visit(typ, payload)
		if isoContainerBoxes[typ] {
			walkIsoBoxes(payload, visit)
		}
		if truncated {
			return
		}
		off += size
	}
}

// parseStsd extracts the codec fourccs of a sample description (stsd) box.
func parseStsd(payload []byte) (videos, audios []string) {
	if len(payload) < 8 {
		return nil, nil
	}
	count := int(binary.BigEndian.Uint32(payload[4:8]))
	for i, off := 0, 8; i < count && off+8 <= len(payload); i++ {
		size := int(binary.BigEndian.Uint32(payload[off : off+4]))
		if size < 8 || off+size > len(payload) {
			break
		}
		if entry, ok := isoSampleEntries[string(payload[off+4:off+8])]; ok {
			if entry.video {
				videos = appendUnique(videos, entry.codec)
			} else {
				audios = appendUnique(audios, entry.codec)
			}
		}
		off += size
	}
	return videos, audios
}

//
// EBML / Matroska / WebM
//

// ebmlCodecs scans the buffer for EBML CodecID elements (0x86) and maps the
// codec strings found inside them. Candidate strings are strictly validated
// against the well-known CodecID vocabulary, which keeps false positives out.
func ebmlCodecs(buf []byte) []string {
	var videos, audios []string
	for i := 0; i+2 <= len(buf); i++ {
		if buf[i] != 0x86 {
			continue
		}
		size, n := ebmlVint(buf[i+1:])
		if n == 0 || size < 2 || size > 64 {
			continue
		}
		start := i + 1 + n
		if start+size > len(buf) {
			continue
		}
		codec, video := ebmlCodec(string(buf[start : start+size]))
		if codec == "" {
			continue
		}
		if video {
			videos = appendUnique(videos, codec)
		} else {
			audios = appendUnique(audios, codec)
		}
	}
	return joinCodecs(videos, audios)
}

// ebmlVint decodes an EBML variable-size integer (data size, not element ID).
func ebmlVint(buf []byte) (size, length int) {
	if len(buf) == 0 {
		return 0, 0
	}
	for length = 1; length <= 4; length++ {
		if buf[0]&(1<<(8-length)) != 0 {
			if len(buf) < length {
				return 0, 0
			}
			size = int(buf[0] & (1<<(8-length) - 1))
			for i := 1; i < length; i++ {
				size = size<<8 | int(buf[i])
			}
			return size, length
		}
	}
	return 0, 0
}

// ebmlCodec maps a Matroska/WebM CodecID string to a codec name. The second
// return value reports whether it is a video codec.
func ebmlCodec(id string) (string, bool) {
	switch {
	case id == "V_VP8":
		return "vp8", true
	case id == "V_VP9":
		return "vp9", true
	case id == "V_AV1":
		return "av1", true
	case id == "V_MPEG4/ISO/AVC":
		return "h264", true
	case id == "V_MPEGH/ISO/HEVC":
		return "hevc", true
	case strings.HasPrefix(id, "V_MPEG4/ISO/"):
		return "mpeg4", true
	case id == "V_MPEG2":
		return "mpeg2", true
	case id == "V_MPEG1":
		return "mpeg1", true
	case id == "V_THEORA":
		return "theora", true
	case id == "V_PRORES":
		return "prores", true
	case strings.HasPrefix(id, "A_AAC"):
		return "aac", false
	case id == "A_MPEG/L3":
		return "mp3", false
	case id == "A_MPEG/L2":
		return "mp2", false
	case id == "A_MPEG/L1":
		return "mp1", false
	case id == "A_OPUS":
		return "opus", false
	case id == "A_VORBIS":
		return "vorbis", false
	case id == "A_FLAC":
		return "flac", false
	case strings.HasPrefix(id, "A_PCM"):
		return "pcm", false
	case id == "A_AC3":
		return "ac3", false
	case id == "A_EAC3":
		return "eac3", false
	case id == "A_DTS":
		return "dts", false
	case id == "A_TRUEHD":
		return "truehd", false
	}
	return "", false
}

//
// RIFF (AVI, WAV)
//

// aviVideoFourccs maps AVI/ASF video stream fourccs to codec names.
var aviVideoFourccs = map[string]string{
	"H264": "h264", "AVC1": "h264", "X264": "h264",
	"HEVC": "hevc", "HVC1": "hevc", "HEV1": "hevc", "H265": "hevc",
	"XVID": "mpeg4", "DIVX": "mpeg4", "DX50": "mpeg4", "FMP4": "mpeg4",
	"MP4V": "mpeg4", "MP4S": "mpeg4", "MP43": "mpeg4", "M4S2": "mpeg4",
	"MJPG": "mjpeg", "MJPA": "mjpeg", "MJPB": "mjpeg", "JPEG": "mjpeg",
	"VP80": "vp8", "VP90": "vp9", "AV01": "av1",
	"WMV1": "wmv1", "WMV2": "wmv2", "WMV3": "wmv3",
	"WVC1": "vc1", "WMVA": "vc1",
	"MPG1": "mpeg1", "MPG2": "mpeg2",
	"FFV1": "ffv1", "HFYU": "huffyuv",
}

func aviVideoFourcc(fcc []byte) string {
	if len(fcc) < 4 {
		return ""
	}
	if fcc[0] == 0 && fcc[1] == 0 && fcc[2] == 0 && fcc[3] == 0 {
		return ""
	}
	return aviVideoFourccs[strings.ToUpper(string(fcc[:4]))]
}

// wavFormatTags maps WAVEFORMATEX format tags to codec names.
var wavFormatTags = map[uint16]string{
	0x0001: "pcm", 0x0003: "pcm", 0xFFFE: "pcm",
	0x0050: "mp3", 0x0055: "mp3",
	0x00FF: "aac",
	0x2000: "ac3",
	0x0160: "wma", 0x0161: "wma", 0x0162: "wma", 0x0163: "wma",
	0x0006: "alaw", 0x0007: "mulaw",
	0x0002: "adpcm", 0x0011: "adpcm",
	0x674F: "opus",
	0xF1AC: "flac",
}

// aviCodecs walks the RIFF chunk tree of an AVI file looking for stream
// header (strh) and stream format (strf) chunks.
func aviCodecs(buf []byte) []string {
	if len(buf) < 12 {
		return nil
	}
	var videos, audios []string

	var walk func(b []byte, stream string)
	walk = func(b []byte, stream string) {
		for off := 0; off+8 <= len(b); {
			id := string(b[off : off+4])
			size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
			data := b[off+8:]
			if size < 0 || size > len(data) {
				size = len(data)
			}
			chunk := data[:size]
			switch id {
			case "LIST", "RIFF":
				if len(chunk) >= 4 {
					walk(chunk[4:], stream)
				}
			case "strh":
				// AVISTREAMHEADER: fccType then fccHandler
				if len(chunk) >= 8 {
					stream = string(chunk[0:4])
					if stream == "vids" {
						videos = appendUnique(videos, aviVideoFourcc(chunk[4:8]))
					}
				}
			case "strf":
				switch stream {
				case "vids":
					// BITMAPINFOHEADER: biCompression at offset 16
					if len(chunk) >= 20 {
						videos = appendUnique(videos, aviVideoFourcc(chunk[16:20]))
					}
				case "auds":
					// WAVEFORMATEX: wFormatTag first
					if len(chunk) >= 2 {
						audios = appendUnique(audios, wavFormatTags[binary.LittleEndian.Uint16(chunk)])
					}
				}
			}
			off += 8 + size + (size & 1) // chunks are word aligned
		}
	}
	walk(buf[12:], "")

	return joinCodecs(videos, audios)
}

// wavCodecs reads the format tag of the fmt chunk of a WAV file.
func wavCodecs(buf []byte) []string {
	if len(buf) < 12 {
		return nil
	}
	for off := 12; off+8 <= len(buf); {
		id := string(buf[off : off+4])
		size := int(binary.LittleEndian.Uint32(buf[off+4 : off+8]))
		data := buf[off+8:]
		if size < 0 || size > len(data) {
			size = len(data)
		}
		if id == "fmt " {
			if size < 2 {
				return nil
			}
			if codec := wavFormatTags[binary.LittleEndian.Uint16(data)]; codec != "" {
				return []string{codec}
			}
			return nil
		}
		off += 8 + size + (size & 1)
	}
	return nil
}

//
// Ogg
//

// oggCodecs identifies the codec of the first logical streams from the
// capture patterns of their first packets.
func oggCodecs(buf []byte) []string {
	limit := 4096
	if len(buf) < limit {
		limit = len(buf)
	}
	head := buf[:limit]

	var videos, audios []string
	if bytes.Contains(head, []byte("\x80theora")) {
		videos = appendUnique(videos, "theora")
	}
	if bytes.Contains(head, []byte("\x01vorbis")) {
		audios = appendUnique(audios, "vorbis")
	}
	if bytes.Contains(head, []byte("OpusHead")) {
		audios = appendUnique(audios, "opus")
	}
	if bytes.Contains(head, []byte("\x7fFLAC")) {
		audios = appendUnique(audios, "flac")
	}
	if bytes.Contains(head, []byte("Speex   ")) {
		audios = appendUnique(audios, "speex")
	}
	return joinCodecs(videos, audios)
}

//
// FLV
//

// flvCodecs inspects the first FLV tags for the video and audio codec IDs.
func flvCodecs(buf []byte) []string {
	if len(buf) < 13 || buf[0] != 'F' || buf[1] != 'L' || buf[2] != 'V' {
		return nil
	}
	hdr := int(binary.BigEndian.Uint32(buf[5:9]))
	if hdr < 9 || hdr+4 > len(buf) {
		return nil
	}

	var videos, audios []string
	video, audio := "", ""
	for pos := hdr + 4; pos+11 <= len(buf) && (video == "" || audio == ""); {
		typ := buf[pos]
		size := int(buf[pos+1])<<16 | int(buf[pos+2])<<8 | int(buf[pos+3])
		data := pos + 11
		if data+size > len(buf) {
			size = len(buf) - data
		}
		if size < 1 {
			break
		}
		switch typ {
		case 9: // video tag
			if video == "" {
				if b := buf[data]; b&0x80 != 0 && size >= 5 {
					// Enhanced RTMP: codec fourcc follows the flags byte
					if entry, ok := isoSampleEntries[string(buf[data+1:data+5])]; ok && entry.video {
						video = entry.codec
					}
				} else {
					switch b & 0x0F {
					case 2:
						video = "h263"
					case 4, 5:
						video = "vp6"
					case 7:
						video = "h264"
					}
				}
			}
		case 8: // audio tag
			if audio == "" {
				switch buf[data] >> 4 {
				case 0:
					audio = "pcm"
				case 1:
					audio = "adpcm"
				case 2, 14:
					audio = "mp3"
				case 10:
					audio = "aac"
				case 11:
					audio = "speex"
				}
			}
		}
		pos = data + size + 4 // skip tag data and PreviousTagSize
	}

	videos = appendUnique(videos, video)
	audios = appendUnique(audios, audio)
	return joinCodecs(videos, audios)
}

//
// MPEG program stream
//

// mpegCodecs distinguishes MPEG-1 from MPEG-2 streams. The video sequence
// layer is authoritative (a sequence extension start code implies MPEG-2);
// the pack header is used as a fallback.
func mpegCodecs(buf []byte) []string {
	limit := 4096
	if len(buf) < limit {
		limit = len(buf)
	}
	sequenceHeader := false
	for i := 0; i+5 < limit; i++ {
		if buf[i] != 0 || buf[i+1] != 0 || buf[i+2] != 1 {
			continue
		}
		switch buf[i+3] {
		case 0xB3: // sequence header
			sequenceHeader = true
		case 0xB5: // extension start code
			if buf[i+4]>>4 == 1 { // sequence extension
				return []string{"mpeg2"}
			}
		}
	}
	if sequenceHeader {
		return []string{"mpeg1"}
	}
	for i := 0; i+5 < limit; i++ {
		if buf[i] == 0 && buf[i+1] == 0 && buf[i+2] == 1 && buf[i+3] == 0xBA {
			if b := buf[i+4]; b&0xC0 == 0x40 {
				return []string{"mpeg2"}
			} else if b&0xF0 == 0x20 {
				return []string{"mpeg1"}
			}
		}
	}
	return nil
}

//
// ASF (wmv)
//

var (
	// ASF Stream Properties Object
	asfStreamPropsGUID = []byte{0x91, 0x07, 0xDC, 0xB7, 0xB7, 0xA9, 0xCF, 0x11,
		0x8E, 0xE6, 0x00, 0xC0, 0x0C, 0x20, 0x53, 0x65}
	// ASF video media type
	asfVideoMediaGUID = []byte{0xC0, 0xEF, 0x19, 0xBC, 0x4D, 0x5B, 0xCF, 0x11,
		0xA8, 0xFD, 0x00, 0x80, 0x5F, 0x5C, 0x44, 0x2B}
	// ASF audio media type
	asfAudioMediaGUID = []byte{0x40, 0x9E, 0x69, 0xF8, 0x4D, 0x5B, 0xCF, 0x11,
		0xA8, 0xFD, 0x00, 0x80, 0x5F, 0x5C, 0x44, 0x2B}
)

// asfCodecs parses the ASF stream properties objects of a WMV file.
func asfCodecs(buf []byte) []string {
	var videos, audios []string
	for off := 0; ; {
		i := bytes.Index(buf[off:], asfStreamPropsGUID)
		if i < 0 {
			break
		}
		i += off
		off = i + 1
		// GUID (16) + object size (8) + stream type GUID (16)
		streamType := i + 24
		if streamType+16 > len(buf) {
			break
		}
		// time offset (8) + type-specific data length (4) +
		// error correction length (4) + flags (2) + reserved (4)
		spec := streamType + 16 + 8 + 4 + 4 + 2 + 4
		if spec > len(buf) {
			break
		}
		specLen := 0
		if streamType+16+8+4 <= len(buf) {
			specLen = int(binary.LittleEndian.Uint32(buf[streamType+16+8:]))
		}
		if spec+specLen > len(buf) || specLen <= 0 {
			specLen = len(buf) - spec
		}
		switch {
		case bytes.Equal(buf[streamType:streamType+16], asfVideoMediaGUID):
			// The codec fourcc lives inside the BITMAPINFOHEADER of the
			// type-specific data; scan it for a known video fourcc.
			for j := spec; j+4 <= spec+specLen; j++ {
				if codec := aviVideoFourcc(buf[j : j+4]); codec != "" {
					videos = appendUnique(videos, codec)
					break
				}
			}
		case bytes.Equal(buf[streamType:streamType+16], asfAudioMediaGUID):
			// The type-specific data of an audio stream is a WAVEFORMATEX
			if spec+2 <= len(buf) {
				audios = appendUnique(audios, wavFormatTags[binary.LittleEndian.Uint16(buf[spec:])])
			}
		}
	}
	return joinCodecs(videos, audios)
}
