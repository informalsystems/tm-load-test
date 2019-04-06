package outagesim

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBringTendermintUp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(
		MakeOutageEndpointHandler(
			func() bool { return false },
			func(string) error { return nil },
		),
	))
	defer ts.Close()

	r, err := http.Post(ts.URL, "text/plain", strings.NewReader("up"))
	if err != nil {
		t.Fatalf("Got unexpected error: %s", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("Expected status code %d, got %d", http.StatusOK, r.StatusCode)
	}
}

func TestBringTendermintUpFailed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(
		MakeOutageEndpointHandler(
			func() bool { return false },
			func(string) error { return fmt.Errorf("Some error occurred") },
		),
	))
	defer ts.Close()

	r, err := http.Post(ts.URL, "text/plain", strings.NewReader("up"))
	if err != nil {
		t.Fatalf("Got unexpected error: %s", err)
	}
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Expected status code %d, got %d", http.StatusInternalServerError, r.StatusCode)
	}
}

func TestBringTendermintDown(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(
		MakeOutageEndpointHandler(
			func() bool { return true },
			func(string) error { return nil },
		),
	))
	defer ts.Close()

	r, err := http.Post(ts.URL, "text/plain", strings.NewReader("down"))
	if err != nil {
		t.Fatalf("Got unexpected error: %s", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("Expected status code %d, got %d", http.StatusOK, r.StatusCode)
	}
}

func TestBringTendermintDownFailed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(
		MakeOutageEndpointHandler(
			func() bool { return true },
			func(string) error { return fmt.Errorf("Some error occurred") },
		),
	))
	defer ts.Close()

	r, err := http.Post(ts.URL, "text/plain", strings.NewReader("down"))
	if err != nil {
		t.Fatalf("Got unexpected error: %s", err)
	}
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Expected status code %d, got %d", http.StatusInternalServerError, r.StatusCode)
	}
}

func TestInvalidHTTPMethod(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(
		MakeOutageEndpointHandler(
			func() bool { return true },
			func(string) error { return nil },
		),
	))
	defer ts.Close()

	r, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Got unexpected error: %s", err)
	}
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("Expected status code %d, got %d", http.StatusMethodNotAllowed, r.StatusCode)
	}
}
