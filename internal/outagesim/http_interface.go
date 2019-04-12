package outagesim

import (
	"fmt"
	"io/ioutil"
	"net/http"
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
func MakeOutageEndpointHandler(isTendermintRunningFn func() bool, executeServiceCmdFn func(string) error) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			if r.Body != nil {
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					respond(w, http.StatusInternalServerError, "Internal server error while reading request body")
				}
				switch string(body) {
				case "up":
					tendermintUp(w, isTendermintRunningFn, executeServiceCmdFn)
				case "down":
					tendermintDown(w, isTendermintRunningFn, executeServiceCmdFn)
				default:
					respond(w, http.StatusBadRequest, "Unrecognised command")
				}
			} else {
				respond(w, http.StatusBadRequest, "Missing command in request")
			}
		} else {
			respond(w, http.StatusMethodNotAllowed, "Unsupported method")
		}
	}
}
