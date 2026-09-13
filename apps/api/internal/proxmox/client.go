// Package proxmox is the typed client of a single Proxmox VE cluster.
//
// One Client per cluster, each with its own http.Client, its own tls.Config
// and its own API token. A cluster is reached through several node URLs, any
// of which can answer the whole API, so the Client fails over between them.
//
// Everything the package returns on failure is an *Error, whose message is
// built from the cluster id, the request path, the HTTP status and the cause —
// never from a response body, a header, or a URL carrying credentials.
package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/config"
)

const (
	// apiPrefix is what PVE mounts its API under. Configured URLs carry no
	// path precisely so that this can be appended without ambiguity.
	apiPrefix = "/api2/json"

	// maxBodyBytes bounds how much of a response is read. /cluster/resources
	// on a large cluster is a few hundred kilobytes; a megabyte is generous.
	// The point is not the typical case but the pathological one: a wedged
	// node, or something that is not PVE at all, answering an endless stream.
	// Without the bound a single bad endpoint would take the daemon down.
	maxBodyBytes = 1 << 20
)

// Client talks to one Proxmox VE cluster.
//
// It is safe for concurrent use: the background poller of the aggregator runs
// several calls at once against the same Client.
//
// SECURITY. The token secret is not a field of this struct. It sits inside the
// authTransport, two pointer hops away, so that printing a Client — %v, %+v,
// %#v — cannot reveal it: fmt follows a pointer only at the top level.
type Client struct {
	// clusterID is the configured identifier, the only cluster designation
	// that ever reaches an error message.
	clusterID string
	// urls are the node endpoints, normalised without a trailing slash.
	urls []string
	// timeout is the budget of a SINGLE attempt. The budget of the call as a
	// whole belongs to the caller's context.
	timeout time.Duration
	// http carries the TLS policy of the cluster and the auth transport.
	http *http.Client

	// mu guards last.
	mu sync.Mutex
	// last is the index of the URL that answered last. It is sticky: the
	// next call starts there rather than at zero, so a cluster whose first
	// node is down is not paid for on every single request.
	last int
}

// New builds the client of one cluster. The configuration is assumed already
// validated by config.Load; the checks here are the ones that would otherwise
// turn into a confusing runtime failure — an empty URL list, a missing token,
// a pinned mode with no certificate pool.
func New(cl config.Cluster) (*Client, error) {
	if cl.ID == "" {
		return nil, errors.New("proxmox: cluster id is required")
	}
	if len(cl.URLs) == 0 {
		return nil, fmt.Errorf("proxmox: cluster %s: at least one url is required", cl.ID)
	}
	urls := make([]string, 0, len(cl.URLs))
	for _, raw := range cl.URLs {
		u := strings.TrimRight(strings.TrimSpace(raw), "/")
		if u == "" {
			return nil, fmt.Errorf("proxmox: cluster %s: empty url", cl.ID)
		}
		urls = append(urls, u)
	}
	if cl.TokenID == "" {
		return nil, fmt.Errorf("proxmox: cluster %s: tokenId is required", cl.ID)
	}
	if cl.Secret.IsEmpty() {
		return nil, fmt.Errorf("proxmox: cluster %s: token secret is empty", cl.ID)
	}
	tlsCfg, err := tlsConfig(cl)
	if err != nil {
		return nil, err
	}
	timeout := cl.RequestTimeout
	if timeout <= 0 {
		timeout = config.DefaultTimeout
	}

	// A transport of our own rather than a clone of http.DefaultTransport:
	// the TLS policy is per cluster, and sharing a connection pool between
	// clusters would mean sharing it between trust policies.
	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          len(urls) * 4,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       tlsCfg,
	}

	return &Client{
		clusterID: cl.ID,
		urls:      urls,
		timeout:   timeout,
		http: &http.Client{
			Transport: &authTransport{Base: base, TokenID: cl.TokenID, Secret: cl.Secret},
			// The PVE API does not redirect. Following one would mean
			// replaying an authenticated request against a host nobody
			// configured, so a 3xx is surfaced as the protocol error it is.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
			// No Timeout here on purpose: the budget of one attempt is
			// applied as a sub-context, and the budget of the call as a
			// whole is the caller's context. An http.Client.Timeout would
			// cut across both and could not tell them apart.
		},
	}, nil
}

// tlsConfig builds the TLS policy of one cluster. The pinned pool is the one
// config built at load time: the CA file is never read again.
func tlsConfig(cl config.Cluster) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	switch cl.TLS.Mode {
	case "", config.TLSModeSystem:
		// RootCAs left nil: the system trust store.
	case config.TLSModePinned:
		if cl.TLS.Pool == nil {
			return nil, fmt.Errorf("proxmox: cluster %s: tls mode %q needs a certificate pool", cl.ID, config.TLSModePinned)
		}
		cfg.RootCAs = cl.TLS.Pool
	case config.TLSModeInsecure:
		// Relaxed for THIS cluster only, never globally. config.Load is
		// expected to have made the operator aware of it by name.
		cfg.InsecureSkipVerify = true
	default:
		return nil, fmt.Errorf("proxmox: cluster %s: unknown tls mode %q", cl.ID, cl.TLS.Mode)
	}
	return cfg, nil
}

// ClusterID returns the configured identifier of the cluster.
func (c *Client) ClusterID() string { return c.clusterID }

// ClusterResources returns /cluster/resources: nodes, guests and storages in a
// single call. Needs Sys.Audit.
//
// Mind the traps documented in types.go: CPU is a fraction, sizes are bytes,
// and a shared storage appears once per node.
func (c *Client) ClusterResources(ctx context.Context) ([]Resource, error) {
	return get[[]Resource](ctx, c, "/cluster/resources")
}

// ClusterStatus returns /cluster/status: the quorum entry and one entry per
// member node. Needs Sys.Audit.
//
// A standalone node returns node entries only, with no "cluster" entry: quorum
// is then unknown rather than lost.
func (c *Client) ClusterStatus(ctx context.Context) ([]ClusterStatusEntry, error) {
	return get[[]ClusterStatusEntry](ctx, c, "/cluster/status")
}

// HAManagerStatus returns /cluster/ha/status/manager_status, the only
// structured source for "this node is in maintenance". Needs Sys.Audit.
//
// The result is never nil on success: a cluster with no HA manager answers
// with an empty or null payload, which means "no node is in maintenance", not
// an error.
func (c *Client) HAManagerStatus(ctx context.Context) (*HAManagerStatus, error) {
	status, err := get[*HAManagerStatus](ctx, c, "/cluster/ha/status/manager_status")
	if err != nil {
		return nil, err
	}
	if status == nil {
		status = &HAManagerStatus{}
	}
	return status, nil
}

// AptUpdates returns the pending packages of one node.
//
// Unlike every other endpoint used here it needs Sys.Modify on /nodes, which a
// read-only audit token does not have: a 403 (KindAuth) means "unknown", never
// "up to date".
func (c *Client) AptUpdates(ctx context.Context, node string) ([]AptUpdate, error) {
	if node == "" {
		return nil, errors.New("proxmox: node name is required")
	}
	return get[[]AptUpdate](ctx, c, "/nodes/"+url.PathEscape(node)+"/apt/update")
}

// NodeStatus returns /nodes/{node}/status: what one node reports about itself,
// which /cluster/resources does not carry — swap, root filesystem, load
// average, running versions. Needs Sys.Audit.
//
// Mind the traps documented on the type: CPU is a fraction, sizes are bytes,
// and loadavg arrives as three strings.
func (c *Client) NodeStatus(ctx context.Context, node string) (*NodeStatus, error) {
	path, err := c.nodePath(node, "/status")
	if err != nil {
		return nil, err
	}
	status, err := get[*NodeStatus](ctx, c, path)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, emptyPayload(c.clusterID, path)
	}
	return status, nil
}

// GuestStatus returns /nodes/{node}/{kind}/{vmid}/status/current for a QEMU VM
// or an LXC container. Needs VM.Audit on the guest.
//
// kind is ResourceTypeQemu or ResourceTypeLXC; anything else is rejected here,
// without a request, since there is no such endpoint to ask.
func (c *Client) GuestStatus(ctx context.Context, node, kind string, vmid int) (*GuestStatus, error) {
	path, err := c.guestPath(node, kind, vmid, "/status/current")
	if err != nil {
		return nil, err
	}
	status, err := get[*GuestStatus](ctx, c, path)
	if err != nil {
		return nil, err
	}
	if status == nil {
		return nil, emptyPayload(c.clusterID, path)
	}
	return status, nil
}

// NodeRRD returns /nodes/{node}/rrddata, the recorded history of a node over
// one of the Timeframe* windows. Needs Sys.Audit.
//
// The consolidation function is AVERAGE, the only one whose points can be
// compared across timeframes: MAX would make an hour of history and a year of
// it two different measurements on the same axis.
//
// Samples come back oldest first, and a field MISSING from a sample is a hole
// in the series, decoded as nil. See RRDPoint.
func (c *Client) NodeRRD(ctx context.Context, node, timeframe string) ([]RRDPoint, error) {
	path, err := c.nodePath(node, "/rrddata")
	if err != nil {
		return nil, err
	}
	query, err := rrdQuery(timeframe)
	if err != nil {
		return nil, err
	}
	return get[[]RRDPoint](ctx, c, path+query)
}

// GuestRRD returns /nodes/{node}/{kind}/{vmid}/rrddata, the recorded history of
// one guest. Needs VM.Audit on the guest. See NodeRRD for the conventions,
// which are the same; only the set of columns differs.
func (c *Client) GuestRRD(ctx context.Context, node, kind string, vmid int, timeframe string) ([]RRDPoint, error) {
	path, err := c.guestPath(node, kind, vmid, "/rrddata")
	if err != nil {
		return nil, err
	}
	query, err := rrdQuery(timeframe)
	if err != nil {
		return nil, err
	}
	return get[[]RRDPoint](ctx, c, path+query)
}

// ClusterTasks returns /cluster/tasks, the recent jobs of the whole cluster,
// most recent first. Needs Sys.Audit.
//
// A RUNNING task comes back without an end time — see the trap on Task.
//
// TRAP. This endpoint takes NO parameter, and takes none strictly: its schema
// is empty and forbids additional properties, so a "limit" is not ignored but
// rejected, with a 400 "Parameter verification failed". Only the per-node
// /nodes/{node}/tasks accepts limit, start and the filters. PVE answers with
// the tail of the cluster task log, which is short already; a caller wanting
// fewer entries cuts the slice itself.
func (c *Client) ClusterTasks(ctx context.Context) ([]Task, error) {
	return get[[]Task](ctx, c, "/cluster/tasks")
}

// GuestIPv4 returns the first non-loopback IPv4 address the QEMU guest agent
// reports for a VM, asking
// /nodes/{node}/qemu/{vmid}/agent/network-get-interfaces.
//
// THIS CALL IS EXPECTED TO FAIL, routinely. The endpoint only answers when the
// guest agent is installed, enabled and running; otherwise PVE replies 500 or
// 501, which surfaces as a KindProtocol error. That is the normal state of a
// VM without an agent, not an incident: the caller renders an unknown address
// and moves on rather than marking the cluster unhealthy.
//
// An LXC container is rejected without a request: containers have no agent
// tree, and their addresses are read from their configuration instead.
// ErrNoGuestIPv4 distinguishes "the agent answered, it knows no usable
// address" from an actual failure.
func (c *Client) GuestIPv4(ctx context.Context, node string, vmid int) (string, error) {
	return c.guestIPv4(ctx, node, ResourceTypeQemu, vmid)
}

// guestIPv4 is the kind-aware body of GuestIPv4.
//
// The exported call takes no kind because the answer only ever comes from
// QEMU, but the rule itself is worth stating once, in code, rather than only
// in a comment: a container reaching here must be turned away before a request
// is built, not after PVE has answered 501 to a path that does not exist.
func (c *Client) guestIPv4(ctx context.Context, node, kind string, vmid int) (string, error) {
	if !GuestKindSupportsAgent(kind) {
		return "", fmt.Errorf("proxmox: guest kind %q has no guest agent, only %q does", kind, ResourceTypeQemu)
	}
	path, err := c.guestPath(node, kind, vmid, "/agent/network-get-interfaces")
	if err != nil {
		return "", err
	}
	// The agent wraps its own payload in a "result" member, inside the
	// regular {"data": ...} envelope the generic getter already removes.
	type agentResult struct {
		Result []guestAgentInterfaces `json:"result"`
	}
	res, err := get[*agentResult](ctx, c, path)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", ErrNoGuestIPv4
	}
	for _, iface := range res.Result {
		for _, addr := range iface.IPAddresses {
			if ip := usableIPv4(addr.Address); ip != "" {
				return ip, nil
			}
		}
	}
	return "", ErrNoGuestIPv4
}

// usableIPv4 returns the address when it is an IPv4 one worth reporting, and
// the empty string otherwise. Loopback, link-local (the 169.254/16 a guest
// gives itself when DHCP failed) and the unspecified address are all skipped:
// none of them is an address anyone can reach the guest on. The type field the
// agent sends is not trusted, the parsed address decides.
func usableIPv4(addr string) string {
	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil || ip.To4() == nil {
		return ""
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() {
		return ""
	}
	return ip.String()
}

// emptyPayload is the error of an endpoint that answered 200 with a null data
// member where an object was expected.
//
// It exists so that no getter of a single object ever returns (nil, nil): a
// caller reading the result of a successful call has every right to
// dereference it, and a nil pointer there would take the daemon down on the
// one malformed answer nobody tested against. The *Error is built by hand
// rather than by Classify, which deduces its Kind from a status and a
// transport error and has neither here — the cluster answered, so this is a
// protocol failure by definition.
func emptyPayload(cluster, path string) *Error {
	return &Error{Cluster: cluster, Path: path, Kind: KindProtocol, Err: errEmptyPayload}
}

// nodePath builds "/nodes/{node}{suffix}" with the node name escaped. A node
// name is operator-supplied data that lands in a URL path: escaping it is what
// keeps a name with a slash in it from reaching a different endpoint.
func (c *Client) nodePath(node, suffix string) (string, error) {
	if node == "" {
		return "", errors.New("proxmox: node name is required")
	}
	return "/nodes/" + url.PathEscape(node) + suffix, nil
}

// guestPath builds "/nodes/{node}/{kind}/{vmid}{suffix}", validating the guest
// kind so that an unknown one fails here rather than as a puzzling 501 from
// PVE — and, more to the point, without spending a request to learn it.
func (c *Client) guestPath(node, kind string, vmid int, suffix string) (string, error) {
	if !ValidGuestKind(kind) {
		return "", fmt.Errorf("proxmox: unknown guest kind %q, want %q or %q", kind, ResourceTypeQemu, ResourceTypeLXC)
	}
	if vmid <= 0 {
		return "", fmt.Errorf("proxmox: invalid vmid %d", vmid)
	}
	path, err := c.nodePath(node, "/"+kind+"/"+strconv.Itoa(vmid))
	if err != nil {
		return "", err
	}
	return path + suffix, nil
}

// rrdQuery builds the query string of an RRD call, validating the timeframe.
//
// The validation is the point: the timeframe comes from an HTTP query
// parameter two layers up, and PVE answers a free-form one with a 400 whose
// body this package deliberately drops — the operator would be left with an
// unexplained protocol error. Rejecting it here also means an arbitrary string
// never reaches a URL this client builds, nor the Path of an *Error, which is
// serialised all the way to the browser: what this appends is provably one of
// five constant pairs.
func rrdQuery(timeframe string) (string, error) {
	if !ValidTimeframe(timeframe) {
		return "", fmt.Errorf("proxmox: unknown timeframe %q, want one of %s, %s, %s, %s, %s",
			timeframe, TimeframeHour, TimeframeDay, TimeframeWeek, TimeframeMonth, TimeframeYear)
	}
	q := url.Values{}
	q.Set("timeframe", timeframe)
	// AVERAGE is the consolidation function every PVE RRD defines.
	q.Set("cf", "AVERAGE")
	return "?" + q.Encode(), nil
}

// envelope is the {"data": ...} wrapper every PVE endpoint replies with.
type envelope[T any] struct {
	Data T `json:"data"`
}

// get performs one GET against the cluster and unwraps the data envelope.
//
// It is a free function rather than a method because Go has no generic
// methods, and it exists so that unwrapping happens exactly once for the whole
// package instead of in every getter.
func get[T any](ctx context.Context, c *Client, path string) (T, error) {
	var zero T
	body, err := c.fetch(ctx, path)
	if err != nil {
		return zero, err
	}
	var env envelope[T]
	if err := json.Unmarshal(body, &env); err != nil {
		// Status 0: the request itself succeeded, what failed is the shape
		// of the answer. Classify reads this as a protocol failure, and the
		// json error names a type and an offset, never a value.
		return zero, Classify(c.clusterID, path, 0, err)
	}
	return env.Data, nil
}

// fetch runs one API call, failing over between the URLs of the cluster.
//
// FAILOVER, and its one exception. The URLs are tried starting from the index
// of the last success, then in order, wrapping around. Another node is tried
// on a connection failure, a TLS failure, an attempt timeout or a 5xx, all of
// which are properties of the node reached. It is NEVER tried on a 4xx: the
// nodes of a cluster share one user database, so a 401 or a 403 is identical
// on all of them, and retrying would only multiply failed authentications —
// which is what fail2ban on the other end is watching for.
//
// BUDGET. Each attempt gets a sub-context bounded by the per-call timeout,
// while the whole sequence stays bounded by the caller's context: once that
// one is done, the next URL is not tried.
//
// The error returned is the one of the last attempt, told how many URLs were
// tried. Only the relative path reaches it, never the URL that produced it.
func (c *Client) fetch(ctx context.Context, path string) ([]byte, error) {
	n := len(c.urls)
	start := c.startIndex()

	var (
		lastErr    error
		lastStatus int
		tried      int
	)
	for i := 0; i < n; i++ {
		if err := ctx.Err(); err != nil {
			if tried == 0 {
				return nil, c.contextError(path, err)
			}
			break
		}
		idx := (start + i) % n
		tried++

		body, status, err := c.attempt(ctx, c.urls[idx], path)
		if err == nil && status >= 200 && status <= 299 {
			c.markSuccess(idx)
			return body, nil
		}
		lastErr, lastStatus = err, status

		// The caller's budget is spent: stop here rather than spending the
		// next node's time on a request whose answer nobody will read.
		if cerr := ctx.Err(); cerr != nil {
			return nil, c.contextError(path, cerr)
		}
		// status 0 means no answer at all (connection, TLS, timeout).
		if !(status == 0 || status >= 500) {
			break
		}
	}

	cause := lastErr
	if n > 1 {
		cause = &triedError{tried: tried, total: n, err: cause}
	}
	return nil, Classify(c.clusterID, path, lastStatus, cause)
}

// attempt runs the request against one URL and returns the body of a
// successful answer, the HTTP status, and the transport or read error.
//
// A status of 0 means the request never got an answer, which is what tells
// fetch it may try another node.
func (c *Client) attempt(ctx context.Context, base, path string) ([]byte, int, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, base+apiPrefix+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	// No Authorization here: the token is the transport's business, and
	// building the header anywhere else is how it ends up somewhere it
	// should not be.

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, unwrapURL(err)
	}
	defer resp.Body.Close()
	// http.Transport hands back the request it sent, Authorization header
	// included. Nothing here reads it, but a response is the kind of value
	// someone prints whole while chasing a bug, and %+v on it would print the
	// token. Dropping it costs nothing: the redirect check has already run,
	// and the response does not leave this function.
	resp.Request = nil

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// The body of a failed answer is READ AND DROPPED. PVE fills it with
		// server-side detail, and anything sitting in front of it — a reverse
		// proxy, a captive portal, a load balancer error page — has been
		// known to echo request headers back, Authorization included. It must
		// not reach an error, which ends up in the JSON of /api/overview.
		// Reading it keeps the connection reusable.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes))
		return nil, resp.StatusCode, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// startIndex returns the URL index the next call should start from.
func (c *Client) startIndex() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// markSuccess makes idx the starting point of subsequent calls.
func (c *Client) markSuccess(idx int) {
	c.mu.Lock()
	c.last = idx
	c.mu.Unlock()
}

// contextError builds the error of a call the caller's context put an end to.
//
// Cancellation and deadline are deliberately reported as the same Kind. From
// where the aggregator stands they are one event — the budget for this cluster
// is gone, keep the last known snapshot — and a cancelled context here only
// ever comes from a deadline set one level up. Classify cannot make that call
// on its own, since context.Canceled carries no notion of time.
func (c *Client) contextError(path string, err error) *Error {
	e := Classify(c.clusterID, path, 0, err)
	e.Kind = KindTimeout
	return e
}

// triedError reports how many of the cluster's URLs were tried before giving
// up, so that "connection refused" reads as the end of a sequence rather than
// as a single unlucky node. It wraps the cause, which may be nil when the last
// node answered with a status and nothing else.
type triedError struct {
	tried int
	total int
	err   error
}

// Error implements error.
func (t *triedError) Error() string {
	s := fmt.Sprintf("tried %d of %d urls", t.tried, t.total)
	if t.err != nil {
		s += ": " + t.err.Error()
	}
	return s
}

// Unwrap keeps errors.Is and errors.As, and therefore Classify, working
// through the annotation.
func (t *triedError) Unwrap() error { return t.err }

// unwrapURL strips the *url.Error that http.Client wraps every transport
// failure in. Its message repeats the full URL, which Error deliberately does
// not carry: the cluster id and the relative path identify the call, and a URL
// names a node the browser has no business learning about. The cause
// underneath is left untouched, so the x509 and net.Error matching done by
// Classify is unaffected.
func unwrapURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
}
