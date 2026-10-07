// Package gen generates deterministic synthetic telemetry, one simulated
// second at a time.
package gen

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/abogoyavlensky/tracelet/spike/storage/internal/telemetry"
)

// Profile fixes a workload's rates and cardinality.
type Profile struct {
	Name         string
	LogsPerSec   int
	SpansPerSec  int
	PointsPerSec int
	Services     int
	Environments int
	Series       int // every series emits once per SeriesInterval
}

// Workload profiles from the design. Busy is the one every threshold is
// checked against.
var (
	Small = Profile{Name: "small", LogsPerSec: 10, SpansPerSec: 5, PointsPerSec: 5, Services: 3, Environments: 1, Series: 50}
	Busy  = Profile{Name: "busy", LogsPerSec: 100, SpansPerSec: 50, PointsPerSec: 50, Services: 10, Environments: 2, Series: 500}
)

// ProfileByName returns the named profile.
func ProfileByName(name string) (Profile, error) {
	switch name {
	case Small.Name:
		return Small, nil
	case Busy.Name:
		return Busy, nil
	default:
		return Profile{}, fmt.Errorf("unknown profile %q", name)
	}
}

// Metric names the generator emits.
const (
	MetricDuration = "http.server.request.duration"
	MetricJobs     = "app.jobs.processed"
	MetricMemory   = "process.memory.usage"
)

const (
	// SeriesInterval is how often every series emits a point.
	SeriesInterval = 10 * time.Second
	// ResetInterval is how often a service instance restarts, resetting its
	// cumulative series.
	ResetInterval = 6 * time.Hour

	project     = "default"
	maxSpans    = 8
	traceShare  = 0.60
	lateShare   = 0.01
	maxLateness = 3 * time.Hour
	minLateness = 10*time.Minute + time.Second
)

// DurationBounds are the OTel default explicit-bucket boundaries.
var DurationBounds = []float64{0, 5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000}

var (
	environments = []string{"prod", "staging"}
	routes       = []string{"/", "/api/users", "/api/users/{id}", "/api/orders", "/api/orders/{id}", "/api/search", "/healthz", "/login"}
	methods      = []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
	jobTypes     = []string{"email", "report", "billing", "cleanup"}
)

type severity struct {
	number int16
	text   string
}

// Severity weights: 70% INFO, 20% WARN, 8% ERROR, 2% DEBUG.
var severities = []struct {
	upTo float64
	sev  severity
}{
	{0.70, severity{9, "INFO"}},
	{0.90, severity{13, "WARN"}},
	{0.98, severity{17, "ERROR"}},
	{1.00, severity{5, "DEBUG"}},
}

type kind int

const (
	kindHistogram kind = iota
	kindSum
	kindGauge
)

// series is one metric series and its cumulative state.
type series struct {
	kind        kind
	name        string
	service     int
	environment string
	attrs       string // JSON text
	hash        uint64
	offset      int64 // emits when unix seconds % 10 == offset

	epoch   int64 // reset epoch the state belongs to; -1 before the first point
	startTS time.Time
	value   int64
	count   uint64
	sum     float64
	min     float64
	max     float64
	buckets []uint64
}

// Generator produces telemetry batches. It is not safe for concurrent use.
type Generator struct {
	profile Profile
	rng     *rand.Rand
	seq     int64
	series  []*series

	resourceDay int64
	version     string            // service version for resourceDay
	resources   map[string]string // service|environment -> resource JSON for resourceDay
}

// NewGenerator returns a generator for the profile. firstSeq is the seq the
// first log gets, so a continued run can resume where the previous one ended.
func NewGenerator(p Profile, seed uint64, firstSeq int64) *Generator {
	g := &Generator{
		profile:     p,
		rng:         rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		seq:         firstSeq,
		resourceDay: -1,
	}
	for i := range p.Series {
		g.series = append(g.series, newSeries(p, i))
	}
	return g
}

func newSeries(p Profile, i int) *series {
	k := kind(i % 3)
	svc := (i / 3) % p.Services
	env := environments[(i/(3*p.Services))%p.Environments]
	idx := i / (3 * p.Services * p.Environments)

	attrs := map[string]string{
		"deployment.environment": env,
		"service.instance.id":    instanceID(svc, env),
	}
	s := &series{kind: k, service: svc, environment: env, offset: int64(i % 10), epoch: -1}
	switch k {
	case kindHistogram:
		s.name = MetricDuration
		attrs["http.route"] = routes[idx%len(routes)]
		attrs["http.request.method"] = methods[(idx/len(routes))%len(methods)]
		attrs["series.index"] = fmt.Sprint(idx)
		s.buckets = make([]uint64, len(DurationBounds)+1)
	case kindSum:
		s.name = MetricJobs
		attrs["job.type"] = jobTypes[idx%len(jobTypes)]
		attrs["series.index"] = fmt.Sprint(idx)
	case kindGauge:
		s.name = MetricMemory
		attrs["series.index"] = fmt.Sprint(idx)
	}
	s.attrs = mustJSON(attrs)
	s.hash = telemetry.SeriesHash(s.name, serviceName(svc), attrs)
	return s
}

// Seq returns the seq the next log will get.
func (g *Generator) Seq() int64 { return g.seq }

// Next generates one simulated second starting at now: traces first, then
// logs, then the metric points due this second.
func (g *Generator) Next(now time.Time) telemetry.Batch {
	now = now.UTC().Truncate(time.Second)
	g.refreshResources(now)

	var b telemetry.Batch
	b.Spans = g.spans(now)
	b.Logs = g.logs(now, b.Spans)
	b.Points = g.points(now)
	return b
}

func (g *Generator) spans(now time.Time) []telemetry.Span {
	spans := make([]telemetry.Span, 0, g.profile.SpansPerSec)
	for len(spans) < g.profile.SpansPerSec {
		size := min(1+g.rng.IntN(maxSpans), g.profile.SpansPerSec-len(spans))
		spans = append(spans, g.trace(now, size)...)
	}
	return spans
}

type spanAttrs struct {
	Route  string `json:"http.route,omitempty"`
	Method string `json:"http.request.method,omitempty"`
	Status int    `json:"http.response.status_code,omitempty"`
	Peer   string `json:"peer.service,omitempty"`
}

func (g *Generator) trace(now time.Time, size int) []telemetry.Span {
	svc := g.rng.IntN(g.profile.Services)
	env := environments[g.rng.IntN(g.profile.Environments)]
	route := routes[g.rng.IntN(len(routes))]
	method := methods[g.rng.IntN(len(methods))]
	traceID := g.hex(2)

	rootStart := now.Add(time.Duration(g.rng.IntN(900)) * time.Millisecond)
	rootDur := time.Duration(5+g.rng.IntN(95)) * time.Millisecond
	status := int16(1) // OK
	httpStatus := 200
	if g.rng.Float64() < 0.02 {
		status, httpStatus = 2, 500
	}

	spans := make([]telemetry.Span, 0, size)
	spans = append(spans, g.span(svc, env, traceID, "", rootStart, rootDur,
		method+" "+route, 2, status, mustJSON(spanAttrs{Route: route, Method: method, Status: httpStatus})))

	for len(spans) < size {
		parent := spans[g.rng.IntN(len(spans))]
		offset := time.Duration(g.rng.Int64N(max(parent.DurationNS/2, 1)))
		dur := time.Duration(g.rng.Int64N(max(parent.DurationNS/2, 1)) + 1)
		peer := serviceName(g.rng.IntN(g.profile.Services))
		spans = append(spans, g.span(svc, env, traceID, parent.SpanID, parent.TS.Add(offset), dur,
			"call "+peer, 3, 0, mustJSON(spanAttrs{Peer: peer})))
	}
	return spans
}

func (g *Generator) span(svc int, env, traceID, parentID string, start time.Time, dur time.Duration,
	name string, kind, status int16, attrs string,
) telemetry.Span {
	return telemetry.Span{
		TS:           start,
		EndTS:        start.Add(dur),
		Project:      project,
		Service:      serviceName(svc),
		Environment:  env,
		Version:      g.version,
		TraceID:      traceID,
		SpanID:       g.hex(1),
		ParentSpanID: parentID,
		Name:         name,
		Kind:         kind,
		StatusCode:   status,
		DurationNS:   int64(dur),
		Scope:        "tracelet.spike",
		Resource:     g.resource(svc, env),
		Attributes:   attrs,
	}
}

type logAttrs struct {
	Seq      int64  `json:"seq"`
	Route    string `json:"http.route"`
	Function string `json:"code.function"`
	UserID   int    `json:"user.id"`
}

func (g *Generator) logs(now time.Time, spans []telemetry.Span) []telemetry.Log {
	logs := make([]telemetry.Log, 0, g.profile.LogsPerSec)
	for range g.profile.LogsPerSec {
		svc := g.rng.IntN(g.profile.Services)
		env := environments[g.rng.IntN(g.profile.Environments)]
		route := routes[g.rng.IntN(len(routes))]
		var traceID, spanID string
		if len(spans) > 0 && g.rng.Float64() < traceShare {
			s := spans[g.rng.IntN(len(spans))]
			traceID, spanID = s.TraceID, s.SpanID
			svc, env = serviceIndex(s.Service), s.Environment
		}

		ts := now.Add(time.Duration(g.rng.IntN(1000)) * time.Millisecond)
		if g.rng.Float64() < lateShare {
			ts = now.Add(-minLateness - time.Duration(g.rng.Int64N(int64(maxLateness-minLateness))))
		}

		sev := g.severity()
		logs = append(logs, telemetry.Log{
			TS:             ts,
			ObservedTS:     ts,
			Project:        project,
			Service:        serviceName(svc),
			Environment:    env,
			Version:        g.version, // the emitting process's version, even for late rows
			SeverityNumber: sev.number,
			SeverityText:   sev.text,
			Body:           fmt.Sprintf("%s handled %s for user %d", serviceName(svc), route, g.rng.IntN(10000)),
			TraceID:        traceID,
			SpanID:         spanID,
			Scope:          "tracelet.spike",
			Resource:       g.resource(svc, env),
			Attributes: mustJSON(logAttrs{
				Seq: g.seq, Route: route, Function: "handle", UserID: g.rng.IntN(10000),
			}),
		})
		g.seq++
	}
	return logs
}

func (g *Generator) severity() severity {
	r := g.rng.Float64()
	for _, s := range severities {
		if r < s.upTo {
			return s.sev
		}
	}
	return severities[len(severities)-1].sev
}

func (g *Generator) points(now time.Time) []telemetry.MetricPoint {
	unix := now.Unix()
	slot := unix % int64(SeriesInterval/time.Second)
	points := make([]telemetry.MetricPoint, 0, g.profile.PointsPerSec)
	for _, s := range g.series {
		if s.offset != slot {
			continue
		}
		points = append(points, g.point(s, now))
	}
	return points
}

func (g *Generator) point(s *series, now time.Time) telemetry.MetricPoint {
	if e := resetEpoch(s.service, now); e != s.epoch {
		s.epoch = e
		s.startTS = now
		s.value, s.count, s.sum, s.min, s.max = 0, 0, 0, 0, 0
		clear(s.buckets)
	}

	p := telemetry.MetricPoint{
		TS:          now,
		Project:     project,
		Service:     serviceName(s.service),
		Environment: s.environment,
		Version:     g.version,
		Name:        s.name,
		SeriesHash:  s.hash,
		Resource:    g.resource(s.service, s.environment),
		Attributes:  s.attrs,
	}

	switch s.kind {
	case kindHistogram:
		for range 1 + g.rng.IntN(20) {
			ms := math.Exp(g.rng.NormFloat64()*1.0 + 3.5) // median ~33 ms
			s.count++
			s.sum += ms
			if s.count == 1 || ms < s.min {
				s.min = ms
			}
			if ms > s.max {
				s.max = ms
			}
			s.buckets[bucketIndex(ms)]++
		}
		p.Unit = "ms"
		p.Type = telemetry.TypeHistogram
		p.Temporality = telemetry.TemporalityCumulative
		p.StartTS = s.startTS
		p.Count = new(s.count)
		p.Sum = new(s.sum)
		p.Min = new(s.min)
		p.Max = new(s.max)
		p.BucketCounts = append([]uint64(nil), s.buckets...)
		p.BucketBounds = DurationBounds
	case kindSum:
		s.value += 1 + g.rng.Int64N(10)
		p.Unit = "{job}"
		p.Type = telemetry.TypeSum
		p.Temporality = telemetry.TemporalityCumulative
		p.Monotonic = true
		p.StartTS = s.startTS
		p.ValueInt = new(s.value)
	case kindGauge:
		p.Unit = "By"
		p.Type = telemetry.TypeGauge
		p.ValueInt = new(int64(100<<20 + g.rng.IntN(50<<20)))
	}
	return p
}

func bucketIndex(v float64) int {
	for i, b := range DurationBounds {
		if v <= b {
			return i
		}
	}
	return len(DurationBounds)
}

// resetEpoch numbers a service instance's lifetimes. Instances restart every
// ResetInterval, staggered per service so resets do not all coincide.
func resetEpoch(service int, now time.Time) int64 {
	period := int64(ResetInterval / time.Second)
	phase := int64(service) * 1777 % period
	return (now.Unix() + phase) / period
}

type resourceAttrs struct {
	ServiceName string `json:"service.name"`
	Environment string `json:"deployment.environment"`
	Version     string `json:"service.version"`
	Instance    string `json:"service.instance.id"`
	SDK         string `json:"telemetry.sdk.language"`
}

// refreshResources rebuilds the cached resource JSON when the simulated day,
// and with it the service version, changes.
func (g *Generator) refreshResources(now time.Time) {
	day := now.Unix() / 86400
	if day == g.resourceDay {
		return
	}
	g.resourceDay = day
	g.version = version(now)
	g.resources = make(map[string]string)
	for svc := range g.profile.Services {
		for _, env := range environments[:g.profile.Environments] {
			g.resources[serviceName(svc)+"|"+env] = mustJSON(resourceAttrs{
				ServiceName: serviceName(svc), Environment: env, Version: g.version,
				Instance: instanceID(svc, env), SDK: "go",
			})
		}
	}
}

func (g *Generator) resource(svc int, env string) string {
	return g.resources[serviceName(svc)+"|"+env]
}

func (g *Generator) hex(words int) string {
	var s string
	for range words {
		s += fmt.Sprintf("%016x", g.rng.Uint64())
	}
	return s
}

func serviceName(i int) string { return fmt.Sprintf("svc-%02d", i+1) }

func serviceIndex(name string) int {
	var i int
	_, _ = fmt.Sscanf(name, "svc-%d", &i)
	return i - 1
}

func instanceID(svc int, env string) string { return fmt.Sprintf("%s-%s-1", serviceName(svc), env) }

// version changes every simulated day.
func version(t time.Time) string { return fmt.Sprintf("1.0.%d", t.Unix()/86400%1000) }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshal %T: %v", v, err)) // only fixed, marshalable types reach here
	}
	return string(b)
}
