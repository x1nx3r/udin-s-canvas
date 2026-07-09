package assets

import (
	"crypto/sha256"
	"embed"
	"fmt"
)

//go:embed globals.css.output
var CSS []byte

//go:embed public/*
var Public embed.FS

// CSSHash is a sha256 hash of the embedded CSS, used for cache busting.
var CSSHash string

func init() {
	h := sha256.Sum256(CSS)
	CSSHash = fmt.Sprintf("%x", h[:8]) // first 8 bytes = 16 hex chars
}
