package loadtest_test

import (
	"encoding/binary"
	"testing"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
)

func TestKVStoreClientFactoryConfigValidation(t *testing.T) {
	testCases := []struct {
		config loadtest.Config
		err    bool
	}{
		{loadtest.Config{Size: 30}, true},  // tx size is too small
		{loadtest.Config{Size: 40}, false}, // tx size is just right
		{loadtest.Config{Size: 100}, false},
		{loadtest.Config{Size: 250}, false},
	}
	factory := loadtest.NewKVStoreClientFactory()
	for i, tc := range testCases {
		err := factory.ValidateConfig(tc.config)
		// if we were supposed to get an error
		if tc.err && err == nil {
			t.Errorf("Expected an error from test case %d, but got nil", i)
		} else if !tc.err && err != nil {
			t.Errorf("Expected no error from test case %d, but got: %v", i, err)
		}
	}
}

func TestKVStoreClient(t *testing.T) {
	testCases := []loadtest.Config {
		{Size: 40},
		{Size: 100},
		{Size: 250},
	}
	factory := loadtest.NewKVStoreClientFactory()
	clientCounter := uint32(0)
	for i, tc := range testCases {
		err := factory.ValidateConfig(tc)
		if err != nil {
			t.Errorf("Expected config from test case %d to validate, but failed: %v", i, err)
		}

		for c := uint32(0); c < 2; c++ {
			client, err := factory.NewClient(tc)
			if err != nil {
				t.Errorf("Did not expect error in test case %d from factory.NewClient: %v", i, err)
			}
			tx, err := client.GenerateTx()
			if err != nil {
				t.Errorf("Did not expect error in test case %d from client %d's GenerateTx: %v", i, c, err)
			}
			if len(tx) != tc.Size {
				t.Errorf("Expected transaction from client %d in test case %d to be %d bytes, but was %d bytes", c, i, tc.Size, len(tx))
			}
			// check that the client's 4-byte ID is embedded in the transaction
			embeddedID := binary.LittleEndian.Uint32(tx[0:4])
			if clientCounter != embeddedID {
				t.Errorf("Expected transaction prefix from client %d in test case %d to be %d, but was %d", c, i, clientCounter, embeddedID)
			}
			clientCounter++
		}
	}
}
