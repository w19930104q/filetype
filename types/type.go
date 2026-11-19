package types

// Type represents a file MIME type and its extension
type Type struct {
	MIME      MIME
	Extension string
	// Codec identifies the codec of the media streams stored inside an
	// audio/video container (e.g. "h264", "vp9", "aac"). It is only
	// populated by codec-level matching (see filetype.MatchCodec); plain
	// container-level matching always leaves it empty.
	Codec string
}

// NewType creates a new Type
func NewType(ext, mime string) Type {
	t := Type{
		MIME:      NewMIME(mime),
		Extension: ext,
	}
	return Add(t)
}
