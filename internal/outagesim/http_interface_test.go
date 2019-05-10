package outagesim

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testUser = "testuser"
const testPassword = "testpassword"
const testPasswordHash = "$2a$08$icFrbtWXmEHXZJ9cZWqQJ.j3DA8r1fHwKs.gXEDpDjN3TzGRFFO.y"

func TestBringTendermintUp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(
		MakeOutageEndpointHandler(
			testUser,
			testPasswordHash,
			func() bool { return false },
			func(string) error { return nil },
		),
	))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	tsURL.User = url.UserPassword(testUser, testPassword)
	r, err := http.Post(tsURL.String(), "text/plain", strings.NewReader("up"))
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
			testUser,
			testPasswordHash,
			func() bool { return false },
			func(string) error { return fmt.Errorf("Some error occurred") },
		),
	))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	tsURL.User = url.UserPassword(testUser, testPassword)
	r, err := http.Post(tsURL.String(), "text/plain", strings.NewReader("up"))
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
			testUser,
			testPasswordHash,
			func() bool { return true },
			func(string) error { return nil },
		),
	))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	tsURL.User = url.UserPassword(testUser, testPassword)
	r, err := http.Post(tsURL.String(), "text/plain", strings.NewReader("down"))
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
			testUser,
			testPasswordHash,
			func() bool { return true },
			func(string) error { return fmt.Errorf("Some error occurred") },
		),
	))
	defer ts.Close()

	tsURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	tsURL.User = url.UserPassword(testUser, testPassword)
	r, err := http.Post(tsURL.String(), "text/plain", strings.NewReader("down"))
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
			testUser,
			testPasswordHash,
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
