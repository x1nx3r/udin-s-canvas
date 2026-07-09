package assets

import "embed"

//go:embed globals.css.output
var CSS []byte

//go:embed public/*
var Public embed.FS
