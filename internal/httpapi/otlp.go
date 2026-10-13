package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/abogoyavlensky/tracelet/internal/ingest"
	"github.com/abogoyavlensky/tracelet/internal/otlp"
	"github.com/abogoyavlensky/tracelet/internal/telemetry"
)

// Ingester accepts converted rows.
type Ingester interface {
	Submit(ctx context.Context, batch telemetry.Batch) error
	RecordRejected(rejected, unknownService int64)
}

// MaxOTLPBody bounds an OTLP request body, before and after decompression.
const MaxOTLPBody = 4 << 20

const protobufType = "application/x-protobuf"

// otlpLogs is OTLP/HTTP logs ingestion.
func (h *Handler) otlpLogs(w http.ResponseWriter, r *http.Request) {
	h.otlpIngest(w, r, "logs", func(body []byte, projectID string) (otlp.Converted, error) {
		req, err := otlp.DecodeLogs(body)
		if err != nil {
			return otlp.Converted{}, err
		}
		return otlp.ConvertLogs(req, projectID), nil
	})
}

// otlpTraces is OTLP/HTTP traces ingestion.
func (h *Handler) otlpTraces(w http.ResponseWriter, r *http.Request) {
	h.otlpIngest(w, r, "traces", func(body []byte, projectID string) (otlp.Converted, error) {
		req, err := otlp.DecodeTraces(body)
		if err != nil {
			return otlp.Converted{}, err
		}
		return otlp.ConvertSpans(req, projectID), nil
	})
}

// otlpMetrics is OTLP/HTTP metrics ingestion.
func (h *Handler) otlpMetrics(w http.ResponseWriter, r *http.Request) {
	h.otlpIngest(w, r, "metrics", func(body []byte, projectID string) (otlp.Converted, error) {
		req, err := otlp.DecodeMetrics(body)
		if err != nil {
			return otlp.Converted{}, err
		}
		return otlp.ConvertMetrics(req, projectID), nil
	})
}

// otlpIngest is OTLP/HTTP ingestion of one signal: protobuf only, optionally
// gzipped. convert decodes the body into rows of the project; a decode error
// is a malformed request. The batch is committed before the response, so a
// 200 means stored.
func (h *Handler) otlpIngest(w http.ResponseWriter, r *http.Request, signal string,
	convert func(body []byte, projectID string) (otlp.Converted, error),
) {
	tok := tokenFrom(r.Context())
	if tok.ProjectID == "" {
		writeError(w, http.StatusForbidden, "ingestion needs a project-scoped ingest token")
		return
	}
	if h.diskPressure() {
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusServiceUnavailable, "free disk is below the configured floor; ingestion is paused")
		return
	}

	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case protobufType:
	case "application/json":
		writeError(w, http.StatusUnsupportedMediaType, "OTLP/JSON is not supported yet; send OTLP/HTTP protobuf (application/x-protobuf)")
		return
	default:
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/x-protobuf")
		return
	}

	body, status, msg := readOTLPBody(w, r)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	converted, err := convert(body, string(tok.ProjectID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "malformed OTLP "+signal+" request")
		return
	}

	h.ingester.RecordRejected(converted.Rejected, converted.UnknownService)
	err = h.ingester.Submit(r.Context(), converted.Batch())
	switch {
	case errors.Is(err, ingest.ErrOverloaded), errors.Is(err, ingest.ErrClosed):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "ingestion is busy; retry shortly")
		return
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The client went away; nobody reads this, and the commit may still land.
		writeError(w, http.StatusServiceUnavailable, "request ended before the commit completed")
		return
	case err != nil:
		h.internalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", protobufType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(otlp.PartialSuccess(converted))
}

// readOTLPBody reads the body, gunzipping it when Content-Encoding says so,
// and enforces MaxOTLPBody on both the raw and the decompressed bytes. A
// non-zero status means the request is answered with it and msg.
func readOTLPBody(w http.ResponseWriter, r *http.Request) (body []byte, status int, msg string) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxOTLPBody))
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return nil, http.StatusRequestEntityTooLarge, "request body exceeds 4 MB"
	}
	if err != nil {
		return nil, http.StatusBadRequest, "could not read request body"
	}

	switch enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
		return raw, 0, ""
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, http.StatusBadRequest, "invalid gzip body"
		}
		body, err := io.ReadAll(io.LimitReader(zr, MaxOTLPBody+1))
		if err != nil {
			return nil, http.StatusBadRequest, "invalid gzip body"
		}
		if len(body) > MaxOTLPBody {
			return nil, http.StatusRequestEntityTooLarge, "decompressed request body exceeds 4 MB"
		}
		return body, 0, ""
	default:
		return nil, http.StatusUnsupportedMediaType, "Content-Encoding must be gzip or absent"
	}
}
