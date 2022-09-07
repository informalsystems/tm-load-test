package outagesim

import (
	"fmt"
	"io"
	"log"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

func respond(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	fmt.Fprint(w, msg+"\n")
}

func tendermintUp(w http.ResponseWriter, isTendermintRunningFn func() bool, executeServiceCmdFn func(string) error) {
	if !isTendermintRunningFn() {
		if err := executeServiceCmdFn("start"); err != nil {
			respond(w, http.StatusInternalServerError, "Failed to start Tendermint process")
		} else {
			respond(w, http.StatusOK, "Service successfully started")
		}
	} else {
		respond(w, http.StatusOK, "Service is already running")
	}
}

func tendermintDown(w http.ResponseWriter, isTendermintRunningFn func() bool, executeServiceCmdFn func(string) error) {
	if isTendermintRunningFn() {
		if err := executeServiceCmdFn("stop"); err != nil {
			respond(w, http.StatusInternalServerError, "Failed to stop Tendermint process")
		} else {
			respond(w, http.StatusOK, "Service successfully stopped")
		}
	} else {
		respond(w, http.StatusOK, "Service is already stopped")
	}
}

// MakeOutageEndpointHandler creates an HTTP handler for dealing with the
// incoming up/down requests to bring the Tendermint service up or down. Sending
// a POST request to the server with either "up" or "down" in the body of the
// request will attempt to bring the Tendermint service up or down accordingly.
func MakeOutageEndpointHandler(
	username, passwordHash string,
	isTendermintRunningFn func() bool,
	executeServiceCmdFn func(string) error,
) func(http.ResponseWriter, *http.Request) {
	log.Printf("Creating outage simulator endpoint: username=%s passwordHash=%s", username, passwordHash)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			respond(w, http.StatusMethodNotAllowed, "Unsupported method")
			return
		}
		if r.Body == nil {
			respond(w, http.StatusBadRequest, "Missing command in request")
			return
		}
		if err := authenticate(r, username, passwordHash); err != nil {
			log.Printf("Failed authentication attempt from: %s", r.RemoteAddr)
			respond(w, http.StatusUnauthorized, fmt.Sprintf("Error: %v", err))
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			respond(w, http.StatusInternalServerError, "Internal server error while reading request body")
			return
		}
		switch string(body) {
		case "up":
			log.Printf("Attempting to bring Tendermint UP")
			tendermintUp(w, isTendermintRunningFn, executeServiceCmdFn)
		case "down":
			log.Printf("Attempting to bring Tendermint DOWN")
			tendermintDown(w, isTendermintRunningFn, executeServiceCmdFn)
		default:
			respond(w, http.StatusBadRequest, "Unrecognised command")
		}
	}
}

func authenticate(req *http.Request, username, passwordHash string) error {
	u, p, ok := req.BasicAuth()
	if !ok {
		return fmt.Errorf("missing username and/or password in request")
	}
	if u != username {
		return fmt.Errorf("invalid username and/or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(p)); err != nil {
		return fmt.Errorf("invalid username and/or password")
	}
	return nil
}
