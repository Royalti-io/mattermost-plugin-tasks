package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	webhookQueueSize = 1000
	webhookTimeout   = 5 * time.Second
	webhookAttempts  = 4
)

// webhookSender delivers signed task events to one configured URL. Delivery is
// best effort and in order: a single worker retries a failing event with backoff
// before moving on, and events are dropped (and logged) if the queue is full, so
// a slow receiver can never block task changes.
type webhookSender struct {
	mu      sync.RWMutex
	url     string
	secret  string
	queue   chan webhookDelivery
	client  *http.Client
	backoff []time.Duration
	logf    func(msg string, keyValuePairs ...any)
	done    chan struct{}
	closing sync.Once
	wg      sync.WaitGroup
}

type webhookDelivery struct {
	id    string
	event string
	body  []byte
}

func newWebhookSender(logf func(msg string, keyValuePairs ...any)) *webhookSender {
	w := &webhookSender{
		queue:   make(chan webhookDelivery, webhookQueueSize),
		client:  &http.Client{Timeout: webhookTimeout},
		backoff: []time.Duration{time.Second, 4 * time.Second, 16 * time.Second},
		logf:    logf,
		done:    make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// Configure sets the target. An empty or non-http(s) URL disables delivery.
func (w *webhookSender) Configure(rawURL, secret string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.secret = secret
	w.url = ""
	if rawURL == "" {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		w.logf("Ignoring invalid webhook URL; it must be http or https")
		return
	}
	w.url = rawURL
}

// Enabled reports whether a target is configured.
func (w *webhookSender) Enabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.url != ""
}

// Enqueue schedules a delivery. It never blocks; it returns false when delivery
// is disabled or the queue is full.
func (w *webhookSender) Enqueue(id, event string, body []byte) bool {
	if !w.Enabled() {
		return false
	}
	select {
	case w.queue <- webhookDelivery{id: id, event: event, body: body}:
		return true
	default:
		w.logf("Webhook queue full; dropping event", "event", event, "delivery", id)
		return false
	}
}

// Close stops the worker. Events still queued are abandoned.
func (w *webhookSender) Close() {
	w.closing.Do(func() { close(w.done) })
	w.wg.Wait()
}

func (w *webhookSender) run() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case d := <-w.queue:
			w.deliver(d)
		}
	}
}

func (w *webhookSender) deliver(d webhookDelivery) {
	for attempt := 0; attempt < webhookAttempts; attempt++ {
		if attempt > 0 {
			wait := w.backoff[min(attempt-1, len(w.backoff)-1)]
			select {
			case <-w.done:
				return
			case <-time.After(wait):
			}
		}

		w.mu.RLock()
		target, secret := w.url, w.secret
		w.mu.RUnlock()
		if target == "" {
			return // disabled while queued
		}

		status, err := w.post(target, secret, d)
		if err == nil && status >= 200 && status < 300 {
			return
		}
		w.logf("Webhook delivery failed", "event", d.event, "delivery", d.id, "attempt", attempt+1, "status", status, "error", errString(err))
	}
	w.logf("Giving up on webhook delivery", "event", d.event, "delivery", d.id)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (w *webhookSender) post(target, secret string, d webhookDelivery) (int, error) {
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(d.body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mattermost-plugin-tasks")
	req.Header.Set("X-Tasks-Event", d.event)
	req.Header.Set("X-Tasks-Delivery", d.id)
	if secret != "" {
		req.Header.Set("X-Tasks-Signature", signBody(secret, d.body))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// signBody returns "sha256=<hex HMAC-SHA256 of body>", the value of X-Tasks-Signature.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
