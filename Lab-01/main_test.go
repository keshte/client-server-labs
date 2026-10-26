package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// MockChecker імітує опитування для тестування без залежності від мережі
type MockChecker struct {
	responses map[string]Result
	delay     time.Duration
}

func (m *MockChecker) Check(url string) Result {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if res, exists := m.responses[url]; exists {
		return res
	}
	return Result{
		URL:      url,
		Duration: m.delay,
		Err:      fmt.Errorf("not found"),
	}
}

func TestInterfaceImplementation(t *testing.T) {
	// Перевірка реалізації інтерфейсу на етапі компіляції
	var _ HealthChecker = (*HTTPChecker)(nil)
	var _ HealthChecker = (*MockChecker)(nil)
}

func TestCheckSequentially(t *testing.T) {
	mock := &MockChecker{
		responses: map[string]Result{
			"http://test1.com": {URL: "http://test1.com", StatusCode: 200},
			"http://test2.com": {URL: "http://test2.com", StatusCode: 404},
		},
		delay: 10 * time.Millisecond,
	}

	urls := []string{"http://test1.com", "http://test2.com"}
	results, duration := CheckSequentially(mock, urls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if duration < 20*time.Millisecond {
		t.Errorf("sequential run took too little time: %v", duration)
	}
}

func TestCheckConcurrentMutex(t *testing.T) {
	mock := &MockChecker{
		responses: map[string]Result{
			"http://ok1.com": {URL: "http://ok1.com", StatusCode: 200},
			"http://ok2.com": {URL: "http://ok2.com", StatusCode: 200},
			"http://err.com": {URL: "http://err.com", Err: fmt.Errorf("network error")},
		},
	}

	urls := []string{"http://ok1.com", "http://ok2.com", "http://err.com"}
	stats, _ := CheckConcurrentMutex(mock, urls)

	if stats.Total != 3 {
		t.Errorf("expected total 3, got %d", stats.Total)
	}
	if stats.Successful != 2 {
		t.Errorf("expected successful 2, got %d", stats.Successful)
	}
	if stats.Errors != 1 {
		t.Errorf("expected errors 1, got %d", stats.Errors)
	}
	if stats.Status200 != 2 {
		t.Errorf("expected status 200 count 2, got %d", stats.Status200)
	}
}

func TestCheckConcurrentChannels(t *testing.T) {
	mock := &MockChecker{
		responses: map[string]Result{
			"http://site1.com": {URL: "http://site1.com", StatusCode: 200},
			"http://site2.com": {URL: "http://site2.com", StatusCode: 500},
			"http://site3.com": {URL: "http://site3.com", Err: fmt.Errorf("timeout")},
		},
	}

	urls := []string{"http://site1.com", "http://site2.com", "http://site3.com"}
	collected := make([]Result, 0)
	stats, _ := CheckConcurrentChannels(mock, urls, func(r Result) {
		collected = append(collected, r)
	})

	if stats.Total != 3 {
		t.Errorf("expected total 3, got %d", stats.Total)
	}
	if len(collected) != 3 {
		t.Errorf("expected collected 3 results, got %d", len(collected))
	}
	if stats.Errors != 1 {
		t.Errorf("expected errors 1, got %d", stats.Errors)
	}
}

func TestHTTPCheckerWithLocalServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()

	checker := NewHTTPChecker(2 * time.Second)
	res := checker.Check(ts.URL)

	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", res.StatusCode)
	}
	if res.Duration <= 0 {
		t.Errorf("duration should be positive, got %v", res.Duration)
	}
}
