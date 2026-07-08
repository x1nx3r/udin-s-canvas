package assets

import "embed"

//go:embed globals.css.output
var CSS []byte

//go:embed docs/*.md
var Docs embed.FS

//go:embed public/*
var Public embed.FS
