// Package dashboard holds saved dashboards: per-project documents of metric
// panels, their validation and normalization, and the revision rules that
// keep concurrent writers from overwriting each other.
package dashboard

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/abogoyavlensky/tracelet/internal/query"
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

// Panel types.
const (
	TypeTimeseries = "timeseries"
	TypeStat       = "stat"
	TypeTable      = "table"
)

// Reductions of a stat or table panel's steps to one value.
const (
	ReduceLast = "last" // the latest complete step
	ReduceSum  = "sum"  // every step added up; only for increase
)

// Limits and defaults.
const (
	SchemaVersion  = 1
	MaxPanels      = 30
	DefaultRefresh = "30s"
	DefaultLast    = "1h" // the range a dashboard opens with when it names none, as query.DefaultLast
	maxTitle       = 200
	maxField       = 255
)

// Refreshes are the auto-refresh choices; off means manual only.
var Refreshes = []string{"off", "10s", "30s", "1m", "5m", "15m"}

var (
	uidRE       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	panelIDRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	aggregateRE = regexp.MustCompile(`^(rate|increase|avg|min|max|sum|p[0-9]{1,2}(\.[0-9])?)$`)
)

// ValidateUID checks a dashboard's uid: 1 to 64 lower-case letters, digits,
// and hyphens, starting with a letter or digit.
func ValidateUID(uid string) error {
	if !uidRE.MatchString(uid) {
		return invalid("uid must be 1 to 64 lower-case letters, digits, and hyphens, starting with a letter or digit")
	}
	return nil
}

// Normalize validates def and returns it with every default filled in, so
// equal dashboards have equal JSON. It does not modify def.
func Normalize(def Definition) (Definition, error) {
	out := def
	out.Title = strings.TrimSpace(def.Title)
	if err := checkText("title", out.Title, maxTitle, true); err != nil {
		return Definition{}, err
	}
	switch out.SchemaVersion {
	case 0:
		out.SchemaVersion = SchemaVersion
	case SchemaVersion:
	default:
		return Definition{}, invalid("schema_version %d is not supported; this server reads %d", def.SchemaVersion, SchemaVersion)
	}
	if out.Last == "" {
		out.Last = DefaultLast
	} else if d, err := time.ParseDuration(out.Last); err != nil || d <= 0 {
		return Definition{}, invalid("last must be a positive duration such as 1h or 24h")
	}
	if out.Refresh == "" {
		out.Refresh = DefaultRefresh
	} else if !slices.Contains(Refreshes, out.Refresh) {
		return Definition{}, invalid("refresh must be one of %s", strings.Join(Refreshes, ", "))
	}
	if len(def.Panels) > MaxPanels {
		return Definition{}, invalid("a dashboard holds at most %d panels", MaxPanels)
	}

	taken := map[string]bool{}
	for i, p := range def.Panels {
		if p.ID == "" {
			continue
		}
		if !panelIDRE.MatchString(p.ID) {
			return Definition{}, invalid("panels[%d].id must be 1 to 40 lower-case letters, digits, and hyphens, starting with a letter or digit", i)
		}
		if taken[p.ID] {
			return Definition{}, invalid("panels[%d].id %q is used by another panel", i, p.ID)
		}
		taken[p.ID] = true
	}
	out.Panels = make([]Panel, 0, len(def.Panels))
	next := 1
	for i, p := range def.Panels {
		np, err := normalizePanel(i, p)
		if err != nil {
			return Definition{}, err
		}
		if np.ID == "" {
			for taken["p"+strconv.Itoa(next)] {
				next++
			}
			np.ID = "p" + strconv.Itoa(next)
			taken[np.ID] = true
		}
		out.Panels = append(out.Panels, np)
	}
	return out, nil
}

func normalizePanel(i int, p Panel) (Panel, error) {
	field := func(name string) string { return fmt.Sprintf("panels[%d].%s", i, name) }
	if err := checkText(field("title"), p.Title, maxTitle, false); err != nil {
		return Panel{}, err
	}
	defaultWidth := 2
	switch p.Type {
	case TypeTimeseries, TypeTable:
	case TypeStat:
		defaultWidth = 1
	case "":
		return Panel{}, invalid("%s is required: timeseries, stat, or table", field("type"))
	default:
		return Panel{}, invalid("%s must be timeseries, stat, or table", field("type"))
	}
	if p.Width == 0 {
		p.Width = defaultWidth
	} else if p.Width < 1 || p.Width > 4 {
		return Panel{}, invalid("%s must be 1 to 4", field("width"))
	}
	if p.Metric == nil {
		return Panel{}, invalid("%s is required", field("metric"))
	}
	m, err := normalizeMetric(field("metric"), *p.Metric)
	if err != nil {
		return Panel{}, err
	}
	p.Metric = &m

	if p.Type == TypeTimeseries {
		if p.Reduce != "" {
			return Panel{}, invalid("%s applies only to stat and table panels", field("reduce"))
		}
		return p, nil
	}
	if p.Type == TypeStat && m.GroupBy != "" {
		return Panel{}, invalid("%s: a stat panel shows one number and cannot be grouped; use a table", field("metric.group_by"))
	}
	switch p.Reduce {
	case "":
		p.Reduce = ReduceLast
	case ReduceLast:
	case ReduceSum:
		if m.Aggregate != "increase" {
			return Panel{}, invalid("%s sum needs metric.aggregate increase: only increases add up to a total", field("reduce"))
		}
	default:
		return Panel{}, invalid("%s must be last or sum", field("reduce"))
	}
	return p, nil
}

func normalizeMetric(field string, m MetricQuery) (MetricQuery, error) {
	if err := checkText(field+".name", m.Name, maxField, true); err != nil {
		return MetricQuery{}, err
	}
	if m.Aggregate != "" && !aggregateRE.MatchString(m.Aggregate) {
		return MetricQuery{}, invalid("%s.aggregate must be rate, increase, avg, min, max, sum, or pNN", field)
	}
	for _, f := range []struct{ name, v string }{{"group_by", m.GroupBy}, {"service", m.Service}, {"environment", m.Environment}} {
		if err := checkText(field+"."+f.name, f.v, maxField, false); err != nil {
			return MetricQuery{}, err
		}
	}
	if len(m.Attr) == 0 {
		m.Attr = nil
	} else {
		m.Attr = slices.Clone(m.Attr)
		for j, a := range m.Attr {
			if _, err := query.ParseAttr(a); err != nil {
				return MetricQuery{}, invalid("%s.attr[%d] must be key:value", field, j)
			}
		}
	}
	if m.Step != "" {
		d, err := time.ParseDuration(m.Step)
		if err != nil || d < query.MinStep || d%time.Second != 0 {
			return MetricQuery{}, invalid("%s.step must be whole seconds and at least %s", field, query.MinStep)
		}
	}
	return m, nil
}

func checkText(field, s string, limit int, required bool) error {
	if required && strings.TrimSpace(s) == "" {
		return invalid("%s is required", field)
	}
	if len(s) > limit {
		return invalid("%s must be at most %d characters", field, limit)
	}
	return nil
}
