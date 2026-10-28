package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Royalti-io/mattermost-plugin-tasks/server/tasks"
)

type received struct {
	body    []byte
	headers http.Header
}

// receiver is a test webhook endpoint that records requests and answers with
// whatever status the test scripts.
type receiver struct {
	srv    *httptest.Server
	mu     sync.Mutex
	got    []received
	status func(n int) int
	calls  atomic.Int64
}

func newReceiver(t *testing.T, status func(n int) int) *receiver {
	t.Helper()
	r := &receiver{status: status}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		n := int(r.calls.Add(1))
		r.mu.Lock()
		r.got = append(r.got, received{body: body, headers: req.Header.Clone()})
		r.mu.Unlock()
		w.WriteHeader(r.status(n))
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) received() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]received(nil), r.got...)
}

func newTestSender(t *testing.T) *webhookSender {
	t.Helper()
	w := newWebhookSender(func(string, ...any) {})
	w.backoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond}
	t.Cleanup(w.Close)
	return w
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 3*time.Second, 5*time.Millisecond, msg)
}

func TestWebhookSignsAndDeliversTheBody(t *testing.T) {
	rcv := newReceiver(t, func(int) int { return 200 })
	w := newTestSender(t)
	w.Configure(rcv.srv.URL, "s3cret")

	body := []byte(`{"hello":"world"}`)
	require.True(t, w.Enqueue("d1", "task.created", body))
	eventually(t, func() bool { return len(rcv.received()) == 1 }, "delivery")

	got := rcv.received()[0]
	assert.Equal(t, body, got.body)
	assert.Equal(t, "task.created", got.headers.Get("X-Tasks-Event"))
	assert.Equal(t, "d1", got.headers.Get("X-Tasks-Delivery"))
	assert.Equal(t, "application/json", got.headers.Get("Content-Type"))

	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	assert.Equal(t, "sha256="+hex.EncodeToString(mac.Sum(nil)), got.headers.Get("X-Tasks-Signature"))
}

func TestWebhookWithoutSecretSendsNoSignature(t *testing.T) {
	rcv := newReceiver(t, func(int) int { return 200 })
	w := newTestSender(t)
	w.Configure(rcv.srv.URL, "")
	w.Enqueue("d", "task.updated", []byte(`{}`))
	eventually(t, func() bool { return len(rcv.received()) == 1 }, "delivery")
	assert.Empty(t, rcv.received()[0].headers.Get("X-Tasks-Signature"))
}

func TestSignBodyKnownVector(t *testing.T) {
	// HMAC-SHA256("key", "The quick brown fox jumps over the lazy dog")
	assert.Equal(t,
		"sha256=f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8",
		signBody("key", []byte("The quick brown fox jumps over the lazy dog")))
}

func TestWebhookRetriesThenSucceeds(t *testing.T) {
	rcv := newReceiver(t, func(n int) int {
		if n < 3 {
			return 500
		}
		return 204
	})
	w := newTestSender(t)
	w.Configure(rcv.srv.URL, "")
	w.Enqueue("d", "task.moved", []byte(`{}`))

	eventually(t, func() bool { return rcv.calls.Load() == 3 }, "two failures then a success")
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, int64(3), rcv.calls.Load(), "stops after success")
}

func TestWebhookGivesUpAfterTheAttemptLimit(t *testing.T) {
	rcv := newReceiver(t, func(int) int { return 503 })
	w := newTestSender(t)
	w.Configure(rcv.srv.URL, "")
	w.Enqueue("d", "task.moved", []byte(`{}`))

	eventually(t, func() bool { return rcv.calls.Load() == webhookAttempts }, "all attempts used")
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, int64(webhookAttempts), rcv.calls.Load())
}

func TestWebhookKeepsEventsInOrderAcrossARetry(t *testing.T) {
	rcv := newReceiver(t, func(n int) int {
		if n == 1 {
			return 500 // first event fails once, the second must still wait for it
		}
		return 200
	})
	w := newTestSender(t)
	w.Configure(rcv.srv.URL, "")
	w.Enqueue("first", "task.created", []byte(`{"n":1}`))
	w.Enqueue("second", "task.updated", []byte(`{"n":2}`))

	eventually(t, func() bool { return len(rcv.received()) == 3 }, "retry then second event")
	var ids []string
	for _, g := range rcv.received() {
		ids = append(ids, g.headers.Get("X-Tasks-Delivery"))
	}
	assert.Equal(t, []string{"first", "first", "second"}, ids)
}

func TestWebhookDisabledOrInvalidURLDeliversNothing(t *testing.T) {
	w := newTestSender(t)
	assert.False(t, w.Enabled())
	assert.False(t, w.Enqueue("d", "task.created", []byte(`{}`)))

	for _, bad := range []string{"ftp://example.com/x", "not a url", "http://", "javascript:alert(1)"} {
		w.Configure(bad, "")
		assert.False(t, w.Enabled(), bad)
	}

	rcv := newReceiver(t, func(int) int { return 200 })
	w.Configure(rcv.srv.URL, "")
	assert.True(t, w.Enabled())
	w.Configure("", "")
	assert.False(t, w.Enabled(), "clearing the URL turns it off")
}

func TestWebhookEnqueueNeverBlocksWhenFull(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)

	w := newTestSender(t)
	w.client.Timeout = 50 * time.Millisecond
	w.Configure(srv.URL, "")

	start := time.Now()
	accepted := 0
	for i := 0; i < webhookQueueSize+50; i++ {
		if w.Enqueue("d", "task.updated", []byte(`{}`)) {
			accepted++
		}
	}
	assert.Less(t, time.Since(start), time.Second, "enqueueing is non-blocking")
	assert.GreaterOrEqual(t, accepted, webhookQueueSize)
	assert.Less(t, accepted, webhookQueueSize+50, "overflow is dropped")
}

func TestBuildEvent(t *testing.T) {
	due := int64(1_900_000_000_000)
	task := &tasks.Task{ID: "t1", ChannelID: "c1", Title: "Ship", Status: tasks.StatusTodo, DueAt: &due,
		Assignees: []string{"u1"}, Labels: []string{"x"}, Checklist: []tasks.ChecklistItem{}, Source: "pa", ExternalID: "42"}

	id, eventType, body, err := buildEvent("moved", eventActor{ID: "u9", Username: "agent", IsBot: true}, task)
	require.NoError(t, err)
	assert.Len(t, id, 26)
	assert.Equal(t, "task.moved", eventType)

	var ev map[string]any
	require.NoError(t, json.Unmarshal(body, &ev))
	assert.Equal(t, id, ev["id"])
	assert.Equal(t, "task.moved", ev["type"])
	assert.NotZero(t, ev["occurred_at"])
	assert.Equal(t, map[string]any{"id": "u9", "username": "agent", "is_bot": true}, ev["actor"])
	taskJSON := ev["task"].(map[string]any)
	assert.Equal(t, "t1", taskJSON["id"])
	assert.Equal(t, "pa", taskJSON["source"], "integrations get their own source and external_id back")
	assert.Equal(t, "42", taskJSON["external_id"])
}
