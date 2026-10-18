// Package dashboard holds saved dashboards: per-project documents of metric
// panels, their validation and normalization, and the revision rules that
// keep concurrent writers from overwriting each other.
package dashboard

import (
	"errors"
	"fmt"
	"time"
)

// Errors callers branch on.
var (
	ErrNotFound = errors.New("dashboard not found")
	ErrConflict = errors.New("dashboard conflict")
)

// ConflictError is a write refused because the stored dashboard is not what
// the writer expected; its message is safe to show to the client.
type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string { return e.Message }

// Is makes a ConflictError match ErrConflict.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

func conflict(format string, args ...any) error {
	return &ConflictError{Message: fmt.Sprintf(format, args...)}
}

// ValidationError reports a definition that breaks a rule; its message names
// the field and is safe to show to the client.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Definition is what a dashboard's author writes: everything but its
// identity, revision, and timestamps.
type Definition struct {
	Title         string  `json:"title"`
	SchemaVersion int     `json:"schema_version"`
	Last          string  `json:"last"`
	Refresh       string  `json:"refresh"`
	Panels        []Panel `json:"panels"`
}

// Panel is one chart, number, or table on a dashboard.
type Panel struct {
	ID     string       `json:"id"`
	Title  string       `json:"title,omitempty"`
	Type   string       `json:"type"`             // timeseries, stat, table
	Width  int          `json:"width"`            // 1..4 quarters of the widest grid
	Reduce string       `json:"reduce,omitempty"` // stat and table: last, sum
	Metric *MetricQuery `json:"metric"`
}

// MetricQuery is a panel's metric query: the metric query endpoint's
// parameters without project and range, which the dashboard supplies.
type MetricQuery struct {
	Name        string   `json:"name"`
	Aggregate   string   `json:"aggregate,omitempty"`
	GroupBy     string   `json:"group_by,omitempty"`
	Service     string   `json:"service,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Attr        []string `json:"attr,omitempty"`
	Step        string   `json:"step,omitempty"`
}

// Dashboard is a stored dashboard.
type Dashboard struct {
	ProjectID string
	UID       string
	Revision  int
	CreatedAt time.Time
	UpdatedAt time.Time
	Definition
}

// Summary is a dashboard's row in a list.
type Summary struct {
	UID       string
	Title     string
	Panels    int
	Revision  int
	UpdatedAt time.Time
}
