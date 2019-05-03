package loadtest

import (
	"encoding/json"
	"io"
	"io/ioutil"
)

// remoteSlave encapsulates the current state of a remote slave from the
// perspective of a master.
type remoteSlave struct {
	ID           string     `json:"id"`
	State        slaveState `json:"state"`
	Status       string     `json:"status"`
	Interactions int64      `json:"interactions"`
}

// resMessage is a generic way of representing a message string (either error or
// otherwise).
type resMessage struct {
	Message string `json:"message"`
}

func toJSON(msg interface{}) (string, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func fromJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}

func fromJSONReadCloser(r io.ReadCloser, v interface{}) error {
	defer r.Close()
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	return fromJSON(string(b), v)
}

func (s *remoteSlave) update(o *remoteSlave) {
	s.State = o.State
	s.Status = o.Status
	s.Interactions = o.Interactions
}
