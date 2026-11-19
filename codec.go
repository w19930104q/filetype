package filetype

import (
	"io"
	"os"

	"github.com/h2non/filetype/matchers"
	"github.com/h2non/filetype/types"
)

// codecHeaderSize is the number of leading bytes inspected by
// MatchCodecReader, consistent with MatchReader.
const codecHeaderSize = 8192

// MatchCodec infers the file type of a given buffer like Match does and, for
// audio and video containers, additionally tries to identify the codec of the
// media streams stored inside the container (e.g. "h264" in an "mp4"
// container, or "vp9" in a "webm" container).
//
// The result is always consistent with the container-level Match result: the
// Extension and MIME values are identical, and when the codec cannot be
// determined the returned type is exactly the container-level result with an
// empty Codec field. Detection is limited to the given buffer, so passing a
// larger buffer (e.g. the whole file) improves the chances of resolving the
// codec of containers that store their metadata at the end of the file.
func MatchCodec(buf []byte) (types.Type, error) {
	kind, err := Match(buf)
	if err != nil {
		return kind, err
	}
	if kind == types.Unknown {
		return kind, nil
	}
	kind.Codec = matchers.DetectCodec(kind, buf)
	return kind, nil
}

// MatchCodecFile infers the file type and codec of a file. When the codec
// cannot be resolved from the file header (e.g. the moov box of an MP4 file
// stored at the end of the file), the whole file is inspected before falling
// back to the container-level result.
func MatchCodecFile(filepath string) (types.Type, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return types.Unknown, err
	}
	defer file.Close()

	header := make([]byte, codecHeaderSize)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return types.Unknown, err
	}
	header = header[:n]

	kind, err := MatchCodec(header)
	if err != nil || kind == types.Unknown || kind.Codec != "" {
		return kind, err
	}

	rest, err := io.ReadAll(file)
	if err != nil {
		return kind, nil
	}
	return MatchCodec(append(header, rest...))
}

// MatchCodecReader is a convenient wrapper to MatchCodec() any Reader. It
// inspects the leading bytes of the stream, like MatchReader does.
func MatchCodecReader(reader io.Reader) (types.Type, error) {
	buffer := make([]byte, codecHeaderSize)

	_, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		return types.Unknown, err
	}

	return MatchCodec(buffer)
}

// IsCodec checks if a given buffer matches the given codec name
func IsCodec(buf []byte, codec string) bool {
	kind, err := MatchCodec(buf)
	return err == nil && kind.Codec != "" && kind.Codec == codec
}
