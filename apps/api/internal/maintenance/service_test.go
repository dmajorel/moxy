package maintenance

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmajorel/moxy/apps/api/internal/aggregate"
)

// The test doubles. There is no SSH anywhere in this file, and there must not
// be: the transport is behind Runner precisely so that every rule of this
// package -- which node is picked, when the next one is tried, who is refused
// -- is decided by code a test can drive without a network.

type runCall struct {
	Target  Target
	Cred    Credential
	Command string
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []runCall
	// handle answers one call. n is the 1-based call number, so a handler can
	// fail the first attempt and accept the second.
	handle func(ctx context.Context, call runCall, n int) (*Result, error)
}

func (r *fakeRunner) Run(ctx context.Context, target Target, cred *Credential, command string) (*Result, error) {
	r.mu.Lock()
	call := runCall{Target: target, Command: command}
	if cred != nil {
		call.Cred = *cred
	}
	r.calls = append(r.calls, call)
	n := len(r.calls)
	handle := r.handle
	r.mu.Unlock()

	if handle == nil {
		return &Result{}, nil
	}
	return handle(ctx, call, n)
}

func (r *fakeRunner) snapshot() []runCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runCall(nil), r.calls...)
}

func (r *fakeRunner) nodes() []string {
	names := []string{}
	for _, call := range r.snapshot() {
		names = append(names, call.Target.Node)
	}
	return names
}

// fakeKeys counts its calls and hands out a DIFFERENT credential every time,
// which is what lets a test prove nothing downstream kept one.
type fakeKeys struct {
	mu    sync.Mutex
	calls int
	err   error
	cred  *Credential
}

func (k *fakeKeys) Credential(context.Context) (*Credential, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls++
	if k.err != nil {
		return nil, k.err
	}
	if k.cred != nil {
		return k.cred, nil
	}
	return &Credential{PrivateKeyPEM: []byte(fmt.Sprintf("credential-%d", k.calls))}, nil
}

func (k *fakeKeys) count() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.calls
}

const testCluster = "qualification"

var testClock = time.Date(2026, 9, 16, 9, 12, 4, 0, time.UTC)

func testOptions() Options {
	return Options{
		SSH: SSHOptions{User: "moxy", Port: 2222, DialTimeout: time.Second, RunTimeout: 2 * time.Second},
		Clusters: map[string]ClusterOptions{
			testCluster: {
				Enabled: true,
				Hosts:   map[string]string{"prox-1": "10.0.0.11", "prox-2": "10.0.0.12"},
			},
			"production": {Enabled: false},
		},
		Now: func() time.Time { return testClock },
	}
}

func online(name string) NodeState  { return NodeState{Name: name, Status: aggregate.NodeOnline} }
func offline(name string) NodeState { return NodeState{Name: name, Status: aggregate.NodeOffline} }
func draining(name string) NodeState {
	return NodeState{Name: name, Status: aggregate.NodeMaintenance}
}

// healthy is a cluster that has quorum, an HA manager, and the given nodes.
func healthy(nodes ...NodeState) ClusterState {
	return ClusterState{
		Quorum:    &aggregate.Quorum{Quorate: true, Nodes: len(nodes), Online: len(nodes)},
		HAManager: true,
		Nodes:     nodes,
	}
}

func enableOn(node string, state ClusterState) Request {
	return Request{Cluster: testCluster, Node: node, Action: ActionEnable, State: state}
}

// TestExecuteRunsTheCommandOnAnotherNode is the happy path, and it checks the
// whole shape of what leaves this package: the command, the account, the port,
// the address from the hosts table, and an outcome that says the request went
// through rather than that the node is drained.
func TestExecuteRunsTheCommandOnAnotherNode(t *testing.T) {
	runner := &fakeRunner{handle: func(context.Context, runCall, int) (*Result, error) {
		return &Result{Output: "requesting maintenance mode\n"}, nil
	}}
	keys := &fakeKeys{}
	svc := NewService(testOptions(), keys, runner)

	outcome, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"))))
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}

	if outcome.Via != "prox-2" {
		t.Errorf("Via = %q, want prox-2", outcome.Via)
	}
	if !outcome.Accepted || outcome.AlreadyInState {
		t.Errorf("Accepted/AlreadyInState = %v/%v, want true/false", outcome.Accepted, outcome.AlreadyInState)
	}
	if outcome.Output == nil || *outcome.Output != "requesting maintenance mode\n" {
		t.Errorf("Output = %v, want the command output", outcome.Output)
	}
	if !outcome.RequestedAt.Equal(testClock) {
		t.Errorf("RequestedAt = %v, want the injected clock", outcome.RequestedAt)
	}
	if outcome.Cluster != testCluster || outcome.Node != "prox-1" || outcome.Action != ActionEnable {
		t.Errorf("outcome names %q/%q/%q", outcome.Cluster, outcome.Node, outcome.Action)
	}

	calls := runner.snapshot()
	if len(calls) != 1 {
		t.Fatalf("%d sessions opened, want 1", len(calls))
	}
	want := runCall{
		Target:  Target{Node: "prox-2", Host: "10.0.0.12", Port: 2222, User: "moxy"},
		Cred:    Credential{PrivateKeyPEM: []byte("credential-1")},
		Command: "node-maintenance enable prox-1",
	}
	if calls[0].Target != want.Target {
		t.Errorf("target = %+v, want %+v", calls[0].Target, want.Target)
	}
	if calls[0].Command != want.Command {
		t.Errorf("command = %q, want %q", calls[0].Command, want.Command)
	}
	if string(calls[0].Cred.PrivateKeyPEM) != string(want.Cred.PrivateKeyPEM) {
		t.Errorf("credential = %q, want %q", calls[0].Cred.PrivateKeyPEM, want.Cred.PrivateKeyPEM)
	}
}

// TestExecuteNeverPicksTheTargetNode is the rule that cannot be relaxed: the
// node being drained is often precisely the one about to be switched off.
func TestExecuteNeverPicksTheTargetNode(t *testing.T) {
	// prox-1 sorts first, so a naive "first online node" would pick it.
	state := healthy(online("prox-1"), online("prox-2"), online("prox-3"))
	for _, node := range []string{"prox-1", "prox-2", "prox-3"} {
		t.Run(node, func(t *testing.T) {
			runner := &fakeRunner{}
			svc := NewService(testOptions(), &fakeKeys{}, runner)

			outcome, err := svc.Execute(context.Background(), enableOn(node, state))
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}
			if outcome.Via == node {
				t.Fatalf("the command ran on the target itself (%q)", node)
			}
			for _, call := range runner.snapshot() {
				if call.Target.Node == node {
					t.Fatalf("a session was opened on the target itself (%q)", node)
				}
			}
		})
	}
}

// TestExecuteCandidateOrder pins the order a test can assert on and two
// operators reading the audit log can predict: plainly online nodes first,
// then the ones already draining, each alphabetically. A node without a hosts
// entry is reached at its own name.
func TestExecuteCandidateOrder(t *testing.T) {
	tests := []struct {
		name  string
		state ClusterState
		want  string
		host  string
	}{
		{
			name:  "alphabetical among online nodes",
			state: healthy(online("prox-1"), online("prox-3"), online("prox-2")),
			want:  "prox-2",
			host:  "10.0.0.12",
		},
		{
			name:  "offline nodes are not candidates",
			state: healthy(online("prox-1"), offline("prox-2"), online("prox-3")),
			want:  "prox-3",
			host:  "prox-3", // no hosts entry: reached at its own name
		},
		{
			name:  "unknown nodes are not candidates either",
			state: healthy(online("prox-1"), NodeState{Name: "prox-2", Status: aggregate.NodeUnknown}, online("prox-3")),
			want:  "prox-3",
			host:  "prox-3",
		},
		{
			name:  "a draining node is a candidate, after the online ones",
			state: healthy(online("prox-1"), draining("prox-2"), online("prox-3")),
			want:  "prox-3",
			host:  "prox-3",
		},
		{
			name:  "a draining node is better than nothing",
			state: healthy(online("prox-1"), draining("prox-2"), offline("prox-3")),
			want:  "prox-2",
			host:  "10.0.0.12",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			svc := NewService(testOptions(), &fakeKeys{}, runner)

			outcome, err := svc.Execute(context.Background(), enableOn("prox-1", tc.state))
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}
			if outcome.Via != tc.want {
				t.Errorf("Via = %q, want %q", outcome.Via, tc.want)
			}
			calls := runner.snapshot()
			if len(calls) != 1 {
				t.Fatalf("%d sessions opened, want 1", len(calls))
			}
			if calls[0].Target.Host != tc.host {
				t.Errorf("host = %q, want %q", calls[0].Target.Host, tc.host)
			}
		})
	}
}

// TestExecuteRefusalsBeforeAnySession walks the first table of §6: every line
// here is a refusal that opens no session at all.
func TestExecuteRefusalsBeforeAnySession(t *testing.T) {
	nodes := healthy(online("prox-1"), online("prox-2"))
	tests := []struct {
		name     string
		req      Request
		wantKind Kind
		wantErr  error
	}{
		{
			name:    "unknown cluster",
			req:     Request{Cluster: "nowhere", Node: "prox-1", Action: ActionEnable, State: nodes},
			wantErr: ErrNotFound,
		},
		{
			name:    "cluster without maintenance enabled",
			req:     Request{Cluster: "production", Node: "prox-1", Action: ActionEnable, State: nodes},
			wantErr: ErrNotFound,
		},
		{
			name:    "unknown node",
			req:     enableOn("prox-9", nodes),
			wantErr: ErrNotFound,
		},
		{
			name:    "node name outside the grammar",
			req:     enableOn("prox 1; rm -rf /", ClusterState{HAManager: true, Nodes: []NodeState{{Name: "prox 1; rm -rf /", Status: aggregate.NodeOnline}, online("prox-2")}}),
			wantErr: ErrNotFound,
		},
		{
			name:    "unknown action",
			req:     Request{Cluster: testCluster, Node: "prox-1", Action: "drain", State: nodes},
			wantErr: ErrInvalidAction,
		},
		{
			name:    "empty action",
			req:     Request{Cluster: testCluster, Node: "prox-1", State: nodes},
			wantErr: ErrInvalidAction,
		},
		{
			name: "no quorum",
			req: enableOn("prox-1", ClusterState{
				Quorum:    &aggregate.Quorum{Quorate: false, Nodes: 3, Online: 1},
				HAManager: true,
				Nodes:     []NodeState{online("prox-1"), online("prox-2")},
			}),
			wantKind: KindNoQuorum,
		},
		{
			name: "no HA manager",
			req: enableOn("prox-1", ClusterState{
				Quorum:    &aggregate.Quorum{Quorate: true, Nodes: 2, Online: 2},
				HAManager: false,
				Nodes:     []NodeState{online("prox-1"), online("prox-2")},
			}),
			wantKind: KindNoHAManager,
		},
		{
			name:     "no other node at all",
			req:      enableOn("prox-1", healthy(online("prox-1"))),
			wantKind: KindNoOtherNode,
		},
		{
			name:     "no other node reachable",
			req:      enableOn("prox-1", healthy(online("prox-1"), offline("prox-2"))),
			wantKind: KindNoOtherNode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			keys := &fakeKeys{}
			svc := NewService(testOptions(), keys, runner)

			outcome, err := svc.Execute(context.Background(), tc.req)
			if outcome != nil {
				t.Errorf("outcome = %+v, want nil", outcome)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantKind != "" {
				kind, ok := KindOf(err)
				if !ok || kind != tc.wantKind {
					t.Fatalf("kind = %q (%v), want %q", kind, err, tc.wantKind)
				}
			}
			if calls := runner.snapshot(); len(calls) != 0 {
				t.Errorf("%d sessions opened, want none", len(calls))
			}
			if keys.count() != 0 {
				t.Errorf("the key source was asked %d times, want none", keys.count())
			}
		})
	}
}

// TestExecuteStandaloneNodeHasNoQuorumToLose: a nil quorum is "the question
// does not apply", never "no quorum". It still has nowhere else to run.
func TestExecuteStandaloneNodeHasNoQuorumToLose(t *testing.T) {
	svc := NewService(testOptions(), &fakeKeys{}, &fakeRunner{})
	state := ClusterState{HAManager: true, Nodes: []NodeState{online("prox-1"), online("prox-2")}}

	if _, err := svc.Execute(context.Background(), enableOn("prox-1", state)); err != nil {
		t.Fatalf("Execute() = %v, want success on a cluster with no quorum entry", err)
	}
}

// TestExecuteAlreadyInState: asking for a state the node already holds is a
// success with nothing done, read BEFORE any session.
func TestExecuteAlreadyInState(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		target  NodeState
		already bool
	}{
		{name: "enable on a draining node", action: ActionEnable, target: draining("prox-1"), already: true},
		{name: "enable on an online node", action: ActionEnable, target: online("prox-1")},
		{name: "disable on an online node", action: ActionDisable, target: online("prox-1"), already: true},
		{name: "disable on a draining node", action: ActionDisable, target: draining("prox-1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			keys := &fakeKeys{}
			svc := NewService(testOptions(), keys, runner)

			outcome, err := svc.Execute(context.Background(), Request{
				Cluster: testCluster,
				Node:    "prox-1",
				Action:  tc.action,
				State:   healthy(tc.target, online("prox-2")),
			})
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}
			if outcome.AlreadyInState != tc.already || !outcome.Accepted {
				t.Fatalf("AlreadyInState/Accepted = %v/%v, want %v/true", outcome.AlreadyInState, outcome.Accepted, tc.already)
			}
			sessions := len(runner.snapshot())
			if tc.already {
				if sessions != 0 || outcome.Via != "" || outcome.Output != nil {
					t.Errorf("%d sessions, Via %q, Output %v: nothing should have run", sessions, outcome.Via, outcome.Output)
				}
				if keys.count() != 0 {
					t.Errorf("the key source was asked %d times, want none", keys.count())
				}
			} else if sessions != 1 {
				t.Errorf("%d sessions opened, want 1", sessions)
			}
		})
	}
}

// TestExecuteTransportFailureTriesTheNextNode is one half of the rule.
func TestExecuteTransportFailureTriesTheNextNode(t *testing.T) {
	for _, kind := range []Kind{KindUnreachable, KindHostKeyMismatch, KindAuthFailed, KindTimeout} {
		t.Run(string(kind), func(t *testing.T) {
			runner := &fakeRunner{handle: func(_ context.Context, call runCall, n int) (*Result, error) {
				if n == 1 {
					return nil, &Error{Kind: kind, Node: call.Target.Node}
				}
				return &Result{Output: "ok"}, nil
			}}
			svc := NewService(testOptions(), &fakeKeys{}, runner)

			outcome, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"), online("prox-3"))))
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}
			if got := runner.nodes(); len(got) != 2 || got[0] != "prox-2" || got[1] != "prox-3" {
				t.Fatalf("sessions opened on %v, want prox-2 then prox-3", got)
			}
			if outcome.Via != "prox-3" {
				t.Errorf("Via = %q, want prox-3", outcome.Via)
			}
		})
	}
}

// TestExecuteTransportFailureEverywhere reports the last failure, having tried
// every candidate.
func TestExecuteTransportFailureEverywhere(t *testing.T) {
	runner := &fakeRunner{handle: func(_ context.Context, call runCall, _ int) (*Result, error) {
		return nil, &Error{Kind: KindUnreachable, Node: call.Target.Node}
	}}
	svc := NewService(testOptions(), &fakeKeys{}, runner)

	_, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"), online("prox-3"))))
	kind, ok := KindOf(err)
	if !ok || kind != KindUnreachable {
		t.Fatalf("kind = %q (%v), want %q", kind, err, KindUnreachable)
	}
	if got := runner.nodes(); len(got) != 2 {
		t.Fatalf("sessions opened on %v, want both candidates tried", got)
	}
}

// TestExecuteApplicativeFailureTriesNoOtherNode is the other half, and the
// important one: the node side answered, so the command may have taken effect.
func TestExecuteApplicativeFailureTriesNoOtherNode(t *testing.T) {
	tests := []struct {
		name     string
		exitCode int
		runErr   error
		wantKind Kind
	}{
		{name: "validator refused the grammar", exitCode: 64, wantKind: KindCommandRefused},
		{name: "validator does not know that node", exitCode: 65, wantKind: KindCommandRefused},
		{name: "ha-manager exited non-zero", exitCode: 1, wantKind: KindCommandFailed},
		{name: "ha-manager exited on a signal", exitCode: 143, wantKind: KindCommandFailed},
		{
			name:     "a failure the runner did not classify",
			runErr:   errors.New("something went wrong halfway"),
			wantKind: KindCommandFailed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{handle: func(context.Context, runCall, int) (*Result, error) {
				if tc.runErr != nil {
					return nil, tc.runErr
				}
				return &Result{ExitCode: tc.exitCode, Output: "moxy-maintenance: bad request"}, nil
			}}
			svc := NewService(testOptions(), &fakeKeys{}, runner)

			outcome, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"), online("prox-3"))))
			if outcome != nil {
				t.Errorf("outcome = %+v, want nil", outcome)
			}
			kind, ok := KindOf(err)
			if !ok || kind != tc.wantKind {
				t.Fatalf("kind = %q (%v), want %q", kind, err, tc.wantKind)
			}
			if Retryable(err) {
				t.Error("an applicative failure must not be retryable")
			}
			if got := runner.nodes(); len(got) != 1 {
				t.Fatalf("sessions opened on %v, want the first candidate only", got)
			}
			var failure *Error
			if errors.As(err, &failure) && tc.runErr == nil && failure.ExitCode != tc.exitCode {
				t.Errorf("ExitCode = %d, want %d", failure.ExitCode, tc.exitCode)
			}
		})
	}
}

// TestExecuteKeySourceFailure: no session is opened at all, and the error says
// the key source did not answer rather than blaming a node that is fine.
func TestExecuteKeySourceFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantKind Kind
	}{
		{
			name:     "an unclassified failure is the source not answering",
			err:      errors.New("dial tcp: connection refused"),
			wantKind: KindKeySourceUnavailable,
		},
		{
			name:     "a sealed vault",
			err:      &Error{Kind: KindKeySourceUnavailable},
			wantKind: KindKeySourceUnavailable,
		},
		{
			name:     "a refused role, which only the implementation can tell",
			err:      &Error{Kind: KindKeySourceDenied},
			wantKind: KindKeySourceDenied,
		},
		{
			name:     "a budget that ran out",
			err:      context.DeadlineExceeded,
			wantKind: KindKeySourceUnavailable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			svc := NewService(testOptions(), &fakeKeys{err: tc.err}, runner)

			_, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"))))
			kind, ok := KindOf(err)
			if !ok || kind != tc.wantKind {
				t.Fatalf("kind = %q (%v), want %q", kind, err, tc.wantKind)
			}
			if calls := runner.snapshot(); len(calls) != 0 {
				t.Fatalf("%d sessions opened, want none: no credential, no session", len(calls))
			}
			var failure *Error
			if errors.As(err, &failure) && failure.Node != "" {
				t.Errorf("the error names node %q; a key source belongs to no node", failure.Node)
			}
		})
	}
}

// TestCredentialIsFetchedOncePerExecution: never at start-up, never cached
// between two executions. The provider hands out a different credential every
// time, so a cached one shows up as the wrong bytes at the runner.
func TestCredentialIsFetchedOncePerExecution(t *testing.T) {
	runner := &fakeRunner{}
	keys := &fakeKeys{}
	svc := NewService(testOptions(), keys, runner)
	state := healthy(online("prox-1"), online("prox-2"))

	for i := 1; i <= 3; i++ {
		if _, err := svc.Execute(context.Background(), enableOn("prox-1", state)); err != nil {
			t.Fatalf("execution %d: %v", i, err)
		}
		if keys.count() != i {
			t.Fatalf("after %d executions the key source was asked %d times", i, keys.count())
		}
	}

	for i, call := range runner.snapshot() {
		want := fmt.Sprintf("credential-%d", i+1)
		if string(call.Cred.PrivateKeyPEM) != want {
			t.Errorf("session %d used %q, want %q: a credential was kept", i+1, call.Cred.PrivateKeyPEM, want)
		}
	}
}

// TestCredentialIsFetchedOncePerExecutionAcrossRetries: the retries of one
// execution are retries, not executions of their own.
func TestCredentialIsFetchedOncePerExecutionAcrossRetries(t *testing.T) {
	runner := &fakeRunner{handle: func(_ context.Context, call runCall, n int) (*Result, error) {
		if n == 1 {
			return nil, &Error{Kind: KindUnreachable, Node: call.Target.Node}
		}
		return &Result{}, nil
	}}
	keys := &fakeKeys{}
	svc := NewService(testOptions(), keys, runner)

	if _, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"), online("prox-3")))); err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if keys.count() != 1 {
		t.Fatalf("the key source was asked %d times for one execution, want 1", keys.count())
	}
}

// TestExecuteRefusesASecondExecutionOnTheSameNode.
//
// Deterministic by construction rather than by luck: the runner blocks until
// the test lets it go, so the second call provably happens while the first is
// open. go test -race is unusable here (the detector needs CGO), which is
// exactly why this is written with channels instead of a sleep.
func TestExecuteRefusesASecondExecutionOnTheSameNode(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	runner := &fakeRunner{handle: func(context.Context, runCall, int) (*Result, error) {
		entered <- struct{}{}
		<-release
		return &Result{Output: "ok"}, nil
	}}
	svc := NewService(testOptions(), &fakeKeys{}, runner)
	state := healthy(online("prox-1"), online("prox-2"))

	first := make(chan error, 1)
	go func() {
		_, err := svc.Execute(context.Background(), enableOn("prox-1", state))
		first <- err
	}()
	<-entered // the first execution holds the lock and is inside the runner

	_, err := svc.Execute(context.Background(), enableOn("prox-1", state))
	kind, ok := KindOf(err)
	if !ok || kind != KindAlreadyRunning {
		t.Fatalf("kind = %q (%v), want %q", kind, err, KindAlreadyRunning)
	}

	close(release)
	if err := <-first; err != nil {
		t.Fatalf("the first execution failed: %v", err)
	}
	if calls := runner.snapshot(); len(calls) != 1 {
		t.Fatalf("%d sessions opened, want 1: the refused request must open none", len(calls))
	}

	// The lock is released once the first execution is done, not held forever.
	if _, err := svc.Execute(context.Background(), enableOn("prox-1", state)); err != nil {
		t.Fatalf("after the first execution ended: %v", err)
	}
}

// TestExecuteAllowsConcurrentExecutionsOnDifferentNodes: the lock is per
// (cluster, node), not a global gate on the service.
func TestExecuteAllowsConcurrentExecutionsOnDifferentNodes(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	runner := &fakeRunner{handle: func(context.Context, runCall, int) (*Result, error) {
		entered <- struct{}{}
		<-release
		return &Result{}, nil
	}}
	svc := NewService(testOptions(), &fakeKeys{}, runner)
	state := healthy(online("prox-1"), online("prox-2"), online("prox-3"))

	done := make(chan error, 2)
	for _, node := range []string{"prox-1", "prox-2"} {
		node := node
		go func() {
			_, err := svc.Execute(context.Background(), enableOn(node, state))
			done <- err
		}()
	}
	<-entered
	<-entered // both are inside the runner at the same time

	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("execution %d: %v", i, err)
		}
	}
}

// TestExecuteCapsTheOutput: a talkative node must not fill the memory of a
// daemon that holds the API tokens of the whole estate.
func TestExecuteCapsTheOutput(t *testing.T) {
	runner := &fakeRunner{handle: func(context.Context, runCall, int) (*Result, error) {
		return &Result{Output: strings.Repeat("a", 32<<10)}, nil
	}}
	svc := NewService(testOptions(), &fakeKeys{}, runner)

	outcome, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"))))
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if outcome.Output == nil {
		t.Fatal("Output is nil, want the capped output")
	}
	if len(*outcome.Output) > OutputLimit+len(truncationNotice) {
		t.Errorf("output is %d bytes, want at most %d", len(*outcome.Output), OutputLimit+len(truncationNotice))
	}
	if !strings.HasSuffix(*outcome.Output, truncationNotice) {
		t.Error("a capped output must say that it was capped")
	}
}

func TestCapOutput(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantCut  bool
		wantSame bool
	}{
		{name: "short output is untouched", in: "done\n", wantSame: true},
		{name: "exactly at the limit is untouched", in: strings.Repeat("a", OutputLimit), wantSame: true},
		{name: "one byte over is cut", in: strings.Repeat("a", OutputLimit+1), wantCut: true},
		// A multi-byte rune straddling the limit must not be cut in half: the
		// result still has to be valid UTF-8, or encoding/json quietly
		// replaces it and the last line reads as corrupt.
		{name: "a rune straddling the limit", in: strings.Repeat("a", OutputLimit-1) + "éé", wantCut: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := capOutput(tc.in)
			if tc.wantSame {
				if got != tc.in {
					t.Fatalf("output was changed, want it untouched")
				}
				return
			}
			if tc.wantCut && !strings.HasSuffix(got, truncationNotice) {
				t.Fatalf("output was not marked as truncated")
			}
			if !utf8Valid(got) {
				t.Error("the capped output is not valid UTF-8")
			}
			if len(got) > OutputLimit+len(truncationNotice) {
				t.Errorf("capped output is %d bytes, want at most %d", len(got), OutputLimit+len(truncationNotice))
			}
		})
	}
}

// utf8Valid is here rather than unicode/utf8 so the assertion reads as what is
// being checked: no continuation byte left dangling at the end.
func utf8Valid(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x80 {
			continue
		}
		n := 0
		switch {
		case s[i]&0xE0 == 0xC0:
			n = 1
		case s[i]&0xF0 == 0xE0:
			n = 2
		case s[i]&0xF8 == 0xF0:
			n = 3
		default:
			return false
		}
		if i+n >= len(s) {
			return false
		}
		for j := 1; j <= n; j++ {
			if s[i+j]&0xC0 != 0x80 {
				return false
			}
		}
		i += n
	}
	return true
}

func TestServiceEnabled(t *testing.T) {
	svc := NewService(testOptions(), &fakeKeys{}, &fakeRunner{})
	tests := []struct {
		cluster string
		want    bool
	}{
		{cluster: testCluster, want: true},
		{cluster: "production", want: false},
		{cluster: "nowhere", want: false},
	}
	for _, tc := range tests {
		if got := svc.Enabled(tc.cluster); got != tc.want {
			t.Errorf("Enabled(%q) = %v, want %v", tc.cluster, got, tc.want)
		}
	}
}

func TestSSHOptionsDefaults(t *testing.T) {
	svc := NewService(Options{Clusters: map[string]ClusterOptions{testCluster: {Enabled: true}}}, &fakeKeys{}, &fakeRunner{})
	if svc.ssh.User != defaultUser || svc.ssh.Port != defaultPort {
		t.Errorf("account = %s:%d, want %s:%d", svc.ssh.User, svc.ssh.Port, defaultUser, defaultPort)
	}
	if svc.ssh.DialTimeout != defaultDialTimeout || svc.ssh.RunTimeout != defaultRunTimeout {
		t.Errorf("budgets = %v/%v, want %v/%v", svc.ssh.DialTimeout, svc.ssh.RunTimeout, defaultDialTimeout, defaultRunTimeout)
	}
}

// TestExecuteHonoursTheSessionBudget: the runner is handed a context that
// carries the deadline, and a runner that ignores it still ends up classified
// as a timeout -- which is transport, so the next node is tried.
func TestExecuteHonoursTheSessionBudget(t *testing.T) {
	opts := testOptions()
	opts.SSH.DialTimeout = time.Millisecond
	opts.SSH.RunTimeout = time.Millisecond

	runner := &fakeRunner{handle: func(ctx context.Context, _ runCall, n int) (*Result, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("the runner was called without a deadline")
		}
		if n == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return &Result{}, nil
	}}
	svc := NewService(opts, &fakeKeys{}, runner)

	outcome, err := svc.Execute(context.Background(), enableOn("prox-1", healthy(online("prox-1"), online("prox-2"), online("prox-3"))))
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if outcome.Via != "prox-3" {
		t.Errorf("Via = %q, want prox-3: a timeout is transport, so the next node is tried", outcome.Via)
	}
}

// TestExecuteStopsWhenTheRequestIsAbandoned: the caller went away, so there is
// nothing to try the next node for.
func TestExecuteStopsWhenTheRequestIsAbandoned(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &fakeRunner{handle: func(ctx context.Context, _ runCall, _ int) (*Result, error) {
		cancel()
		return nil, ctx.Err()
	}}
	svc := NewService(testOptions(), &fakeKeys{}, runner)

	_, err := svc.Execute(ctx, enableOn("prox-1", healthy(online("prox-1"), online("prox-2"), online("prox-3"))))
	kind, ok := KindOf(err)
	if !ok || kind != KindTimeout {
		t.Fatalf("kind = %q (%v), want %q", kind, err, KindTimeout)
	}
	if got := runner.nodes(); len(got) != 1 {
		t.Fatalf("sessions opened on %v, want one: the request was abandoned", got)
	}
}

// The ssh-key source. It answers the same credential every time, and that is
// the whole of it.

func TestSSHKeySource(t *testing.T) {
	key := pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: []byte("openssh-key-v1\x00\x00\x00\x00\x04none\x00\x00\x00\x04none"),
	})

	source, err := NewSSHKeySource(key)
	if err != nil {
		t.Fatalf("NewSSHKeySource() = %v", err)
	}
	first, err := source.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential() = %v", err)
	}
	second, err := source.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential() = %v", err)
	}
	if string(first.PrivateKeyPEM) != string(key) || string(second.PrivateKeyPEM) != string(key) {
		t.Error("the source must hand out the key it was built with")
	}
	if first == second {
		t.Error("the source must not hand out the same struct twice: a caller could blank it")
	}
	if first.Certificate != nil {
		t.Error("ssh-key mode presents no certificate")
	}
}

func TestSSHKeySourceRefusals(t *testing.T) {
	encryptedOpenSSH := pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: []byte("openssh-key-v1\x00\x00\x00\x00\x0aaes256-ctr"),
	})
	legacyEncrypted := pem.EncodeToMemory(&pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-128-CBC,0123"},
		Bytes:   []byte("body"),
	})

	tests := []struct {
		name string
		key  []byte
		want error
	}{
		{name: "no key at all", key: nil, want: errEmptyKey},
		{name: "not PEM", key: []byte("ssh-ed25519 AAAA..."), want: errNotPEM},
		{name: "an encrypted OpenSSH key", key: encryptedOpenSSH, want: errEncryptedKey},
		{name: "a legacy encrypted key", key: legacyEncrypted, want: errEncryptedKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source, err := NewSSHKeySource(tc.key)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if source != nil {
				t.Error("a refused key must not produce a source")
			}
		})
	}
}

// TestExecuteWhenADoubleMisbehaves: a runner or a key source that answers
// neither a value nor an error is a bug, and a bug must not read as a success.
func TestExecuteWhenADoubleMisbehaves(t *testing.T) {
	state := healthy(online("prox-1"), online("prox-2"))

	t.Run("a runner that answers nothing", func(t *testing.T) {
		runner := &fakeRunner{handle: func(context.Context, runCall, int) (*Result, error) {
			return nil, nil
		}}
		svc := NewService(testOptions(), &fakeKeys{}, runner)

		_, err := svc.Execute(context.Background(), enableOn("prox-1", state))
		kind, ok := KindOf(err)
		if !ok || kind != KindCommandFailed {
			t.Fatalf("kind = %q (%v), want %q", kind, err, KindCommandFailed)
		}
	})

	t.Run("a key source that answers nothing", func(t *testing.T) {
		runner := &fakeRunner{}
		svc := NewService(testOptions(), &nilKeys{}, runner)

		_, err := svc.Execute(context.Background(), enableOn("prox-1", state))
		kind, ok := KindOf(err)
		if !ok || kind != KindKeySourceUnavailable {
			t.Fatalf("kind = %q (%v), want %q", kind, err, KindKeySourceUnavailable)
		}
		if calls := runner.snapshot(); len(calls) != 0 {
			t.Fatalf("%d sessions opened, want none", len(calls))
		}
	})
}

// nilKeys is the misbehaving key source of the test above.
type nilKeys struct{}

func (nilKeys) Credential(context.Context) (*Credential, error) { return nil, nil }

func TestExitCodeErrorMessage(t *testing.T) {
	// It must say what happened without quoting what the node printed: the
	// output belongs in the output field, not in a string that is logged.
	if got := exitError(64).Error(); got != "command exited non-zero" {
		t.Errorf("Error() = %q", got)
	}
}

func TestValidNodeName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "prox-qual-2201-cit", want: true},
		{name: "node1.example.net", want: true},
		{name: "", want: false},
		{name: "prox 1", want: false},
		{name: "prox;reboot", want: false},
		{name: "prox_1", want: false},
		{name: "../etc", want: false},
	}
	for _, tc := range tests {
		if got := validNodeName(tc.name); got != tc.want {
			t.Errorf("validNodeName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEncryptedPEM(t *testing.T) {
	tests := []struct {
		name  string
		block *pem.Block
		want  bool
	}{
		{
			name:  "a plain OpenSSH key",
			block: &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: []byte("openssh-key-v1\x00\x00\x00\x00\x04none")},
		},
		{
			name:  "an OpenSSH key without the magic, which is not ours to judge",
			block: &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: []byte("whatever")},
		},
		{
			name:  "a PKCS#8 key",
			block: &pem.Block{Type: "PRIVATE KEY", Bytes: []byte("body")},
		},
		{
			name:  "a PKCS#8 encrypted key, which says so in its type",
			block: &pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("body")},
			want:  true,
		},
		{
			name:  "a legacy key with the encryption headers",
			block: &pem.Block{Type: "RSA PRIVATE KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte("body")},
			want:  true,
		},
		{
			name:  "an OpenSSH key with a cipher",
			block: &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: []byte("openssh-key-v1\x00\x00\x00\x00\x0aaes256-ctr")},
			want:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := encryptedPEM(tc.block); got != tc.want {
				t.Errorf("encryptedPEM() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCredentialIsRedacted: this struct crosses a service, a runner and an
// audit path, and the first %v somebody adds for debugging must not print a
// private key.
func TestCredentialIsRedacted(t *testing.T) {
	cred := Credential{PrivateKeyPEM: []byte("-----BEGIN OPENSSH PRIVATE KEY-----")}
	for _, got := range []string{
		fmt.Sprintf("%v", cred),
		fmt.Sprintf("%s", cred),
		fmt.Sprintf("%#v", cred),
		fmt.Sprintf("%v", &cred),
	} {
		if strings.Contains(got, "PRIVATE KEY") {
			t.Errorf("formatted credential %q carries the key", got)
		}
	}
}
