// Package metrics is a minimal Prometheus exposition, written by hand.
//
// The text format is three lines of grammar — a HELP line, a TYPE line, then
// one sample per series — so it costs less to write than the dependency would
// cost to carry. This daemon holds hypervisor tokens and takes no external
// dependency at all; that rule does not bend for a metric.
//
// CARDINALITY IS BOUNDED BY CONSTRUCTION. Every label value used in this
// repository comes from a closed set: a cluster identifier from the
// configuration, one of nine path kinds, one of six outcomes. No node name, no
// vmid, no URL, no host is ever a label — the same rule that keeps them out of
// error messages, for the same reason: /metrics is a document that leaves the
// process.
package metrics

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// contentType is what a Prometheus scraper expects. The version is part of the
// media type and is not optional.
const contentType = "text/plain; version=0.0.4; charset=utf-8"

// Kind of a metric family, as the TYPE line names it.
const (
	kindCounter   = "counter"
	kindGauge     = "gauge"
	kindHistogram = "histogram"
)

// Registry holds the families of one process.
//
// It is safe for concurrent use: the poller writes from its own goroutines
// while a scraper reads.
type Registry struct {
	mu sync.Mutex
	// families are kept in declaration order, not sorted: the exposition
	// groups samples by family, and a stable order makes two scrapes
	// comparable by eye.
	order    []string
	families map[string]*family
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{families: make(map[string]*family)}
}

// Default is the registry of this process.
var Default = New()

// family is one metric name with its label set.
type family struct {
	// reg is the registry guarding this family. Every write goes through its
	// mutex, so a family handed to a caller carries its own synchronisation
	// rather than relying on a package-level one.
	reg              *Registry
	name, help, kind string
	labels           []string
	// buckets are the upper bounds of a histogram, ascending, without the
	// +Inf one, which is implied and rendered last.
	buckets []float64

	series map[string]*series
	keys   []string
}

// series is one labelled sample of a family.
type series struct {
	values []string
	// value is the counter total, the gauge reading, or the running sum of a
	// histogram.
	value float64
	// count and counts are the histogram's observation count and its
	// cumulative per-bucket counts.
	count  uint64
	counts []uint64
}

// CounterVec declares (or finds) a counter family. Declaring the same name
// twice returns the same family, so a caller may declare where it uses.
func (r *Registry) CounterVec(name, help string, labels ...string) *CounterVec {
	return &CounterVec{f: r.family(name, help, kindCounter, nil, labels)}
}

// GaugeVec declares (or finds) a gauge family.
func (r *Registry) GaugeVec(name, help string, labels ...string) *GaugeVec {
	return &GaugeVec{f: r.family(name, help, kindGauge, nil, labels)}
}

// HistogramVec declares (or finds) a histogram family. The buckets are upper
// bounds in ascending order; +Inf is implied.
func (r *Registry) HistogramVec(name, help string, buckets []float64, labels ...string) *HistogramVec {
	return &HistogramVec{f: r.family(name, help, kindHistogram, buckets, labels)}
}

func (r *Registry) family(name, help, kind string, buckets []float64, labels []string) *family {
	r.mu.Lock()
	defer r.mu.Unlock()

	if f, ok := r.families[name]; ok {
		return f
	}
	f := &family{
		reg:     r,
		name:    name,
		help:    help,
		kind:    kind,
		labels:  append([]string(nil), labels...),
		buckets: append([]float64(nil), buckets...),
		series:  make(map[string]*series),
	}
	r.families[name] = f
	r.order = append(r.order, name)
	return f
}

// at returns the series for these label values, creating it on first use.
//
// A caller passing the wrong number of values is a programming error, and it
// is answered here rather than by a panic in a metrics path: a daemon must not
// die because a counter was miswired. The values are padded or cut, which
// shows up in the exposition as an obviously wrong label rather than as an
// outage.
func (f *family) at(values []string) *series {
	if len(values) != len(f.labels) {
		fixed := make([]string, len(f.labels))
		copy(fixed, values)
		for i := range fixed {
			if fixed[i] == "" {
				fixed[i] = "unknown"
			}
		}
		values = fixed
	}
	key := strings.Join(values, "\x00")
	if s, ok := f.series[key]; ok {
		return s
	}
	s := &series{values: append([]string(nil), values...)}
	if f.kind == kindHistogram {
		s.counts = make([]uint64, len(f.buckets))
	}
	f.series[key] = s
	f.keys = append(f.keys, key)
	return s
}

// CounterVec is a family of monotonically increasing counters.
type CounterVec struct{ f *family }

// Inc adds one to the counter of these label values.
func (c *CounterVec) Inc(values ...string) { c.Add(1, values...) }

// Add adds a non-negative amount. A negative one is dropped: a counter that
// went backwards would make every rate() over it nonsense, and the mistake is
// better visible as a flat line than as a spike.
func (c *CounterVec) Add(delta float64, values ...string) {
	if delta < 0 {
		return
	}
	c.f.reg.mu.Lock()
	c.f.at(values).value += delta
	c.f.reg.mu.Unlock()
}

// GaugeVec is a family of readings that go up and down.
type GaugeVec struct{ f *family }

// Set records the current reading.
func (g *GaugeVec) Set(value float64, values ...string) {
	g.f.reg.mu.Lock()
	g.f.at(values).value = value
	g.f.reg.mu.Unlock()
}

// SetTime records an instant as the UNIX seconds Prometheus expects of a
// *_timestamp_seconds gauge.
func (g *GaugeVec) SetTime(at time.Time, values ...string) {
	g.Set(float64(at.UnixNano())/float64(time.Second), values...)
}

// HistogramVec is a family of bucketed observations.
type HistogramVec struct{ f *family }

// Observe records one measurement.
func (h *HistogramVec) Observe(value float64, values ...string) {
	h.f.reg.mu.Lock()
	s := h.f.at(values)
	s.value += value
	s.count++
	for i, upper := range h.f.buckets {
		if value <= upper {
			s.counts[i]++
		}
	}
	h.f.reg.mu.Unlock()
}

// Duration records an elapsed time in seconds, which is the unit Prometheus
// expects and the reason every such metric is named *_seconds.
func (h *HistogramVec) Duration(d time.Duration, values ...string) {
	h.Observe(d.Seconds(), values...)
}

// Text renders the whole registry in the Prometheus text exposition format.
//
// Samples of one family are written together, under one HELP and one TYPE
// line, which is what the format requires; series within a family are sorted
// by their label values so two scrapes of an unchanged process are identical.
func (r *Registry) Text() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var b strings.Builder
	for _, name := range r.order {
		f := r.families[name]
		if len(f.keys) == 0 {
			// A family nobody has written to yet has nothing to say. Emitting
			// its HELP and TYPE alone is valid but noisy, and a scraper reads
			// the absence the same way.
			continue
		}
		b.WriteString("# HELP " + f.name + " " + escapeHelp(f.help) + "\n")
		b.WriteString("# TYPE " + f.name + " " + f.kind + "\n")

		keys := append([]string(nil), f.keys...)
		sort.Strings(keys)
		for _, key := range keys {
			f.writeSeries(&b, f.series[key])
		}
	}
	return b.String()
}

func (f *family) writeSeries(b *strings.Builder, s *series) {
	if f.kind != kindHistogram {
		b.WriteString(f.name + labelsOf(f.labels, s.values, "", "") + " " + formatFloat(s.value) + "\n")
		return
	}
	for i, upper := range f.buckets {
		b.WriteString(f.name + "_bucket" + labelsOf(f.labels, s.values, "le", formatFloat(upper)) +
			" " + strconv.FormatUint(s.counts[i], 10) + "\n")
	}
	// The +Inf bucket is mandatory and always equals the count.
	b.WriteString(f.name + "_bucket" + labelsOf(f.labels, s.values, "le", "+Inf") +
		" " + strconv.FormatUint(s.count, 10) + "\n")
	b.WriteString(f.name + "_sum" + labelsOf(f.labels, s.values, "", "") + " " + formatFloat(s.value) + "\n")
	b.WriteString(f.name + "_count" + labelsOf(f.labels, s.values, "", "") + " " + strconv.FormatUint(s.count, 10) + "\n")
}

// labelsOf renders the label set, optionally with one extra pair appended —
// the "le" of a histogram bucket, which must come last.
func labelsOf(names, values []string, extraName, extraValue string) string {
	if len(names) == 0 && extraName == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("{")
	for i, name := range names {
		if i > 0 {
			b.WriteString(",")
		}
		value := ""
		if i < len(values) {
			value = values[i]
		}
		b.WriteString(name + `="` + escapeLabel(value) + `"`)
	}
	if extraName != "" {
		if len(names) > 0 {
			b.WriteString(",")
		}
		b.WriteString(extraName + `="` + escapeLabel(extraValue) + `"`)
	}
	b.WriteString("}")
	return b.String()
}

// escapeLabel escapes the three characters the format reserves inside a label
// value. Nothing else is touched: a UTF-8 value passes through as it is.
func escapeLabel(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(v)
}

// escapeHelp escapes what a HELP line reserves: the backslash and the newline.
// A quote is ordinary text there.
func escapeHelp(v string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	return r.Replace(v)
}

// formatFloat writes a number the way the exposition format wants it: no
// exponent for the ordinary range, and the shortest form that round-trips.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Handler serves the registry at /metrics.
//
// It answers GET and HEAD only, like every other route of this daemon, and
// carries no caching: a scraper reading a cached page would chart a process
// that has since stopped.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(r.Text()))
	})
}
