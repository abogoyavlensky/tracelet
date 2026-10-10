// Package api holds the hand-written OpenAPI description of the HTTP API.
// It lives next to the file because Go's embed cannot reach a parent
// directory.
package api

import _ "embed"

// OpenAPI is api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPI []byte
