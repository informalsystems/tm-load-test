package loadtest

import (
	"encoding/json"
	"io"
	"io/ioutil"
)

type reqCreateOrUpdateSlave struct {
	State  slaveState `json:"state"`
	Status string     `json:"status,omitempty"`
}

type reqUpdateSlaveInteractions struct {
	Count int64 `json:"count"`
}

type resSlaveRejected struct {
	Reason string `json:"reason"`
}

type resMasterFailed struct {
	Reason string `json:"reason"`
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
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	return fromJSON(string(b), v)
}
