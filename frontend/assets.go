// Package frontend embeds Everlore's server-rendered templates and static assets.
package frontend

import "embed"

// Assets contains the templates and files served by the backend.
//
//go:embed templates/*.html static/*
var Assets embed.FS
