package loadtest_test

import (
	"testing"

	"github.com/informalsystems/tm-load-test/pkg/loadtest"
)

func TestKVStoreClientFactoryConfigValidation(t *testing.T) {
	testCases := []struct {
		config loadtest.Config
		err    bool
	}{
		{loadtest.Config{Size: 1, Rate: 1000, Time: 1000, Count: -1}, true},  // invalid tx size
		{loadtest.Config{Size: 10, Rate: 1000, Time: 1000, Count: -1}, true}, // tx size is too small

		{loadtest.Config{Size: 14, Rate: 1000, Time: 10000, Count: -1}, false}, // just right for parameters

		{loadtest.Config{Size: 20, Rate: 1000, Time: 10, Count: -1}, false},   // 10k txs @ 20 bytes each
		{loadtest.Config{Size: 20, Rate: 1000, Time: 100, Count: -1}, false},  // 100k txs @ 20 bytes each
		{loadtest.Config{Size: 20, Rate: 1000, Time: 1000, Count: -1}, false}, // 1m txs @ 20 bytes each

		{loadtest.Config{Size: 100, Rate: 1000, Time: 10, Count: -1}, false},
		{loadtest.Config{Size: 100, Rate: 1000, Time: 100000, Count: -1}, false}, // 100m txs @ 100 bytes each

		{loadtest.Config{Size: 250, Rate: 1000, Time: 10, Count: -1}, false},

		{loadtest.Config{Size: 10240, Rate: 1000, Time: 10, Count: -1}, false},     // 10k txs @ 10kB each
		{loadtest.Config{Size: 10240, Rate: 1000, Time: 100, Count: -1}, false},    // 100k txs @ 10kB each
		{loadtest.Config{Size: 10240, Rate: 1000, Time: 1000, Count: -1}, false},   // 1m txs @ 10kB each
		{loadtest.Config{Size: 10240, Rate: 1000, Time: 10000, Count: -1}, false},  // 10m txs @ 10kB each
		{loadtest.Config{Size: 10240, Rate: 1000, Time: 100000, Count: -1}, false}, // 100m txs @ 10kB each
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

func benchmarkKVStoreClient_GenerateTx(b *testing.B, cfg loadtest.Config) {
	cfg.Count = b.N
	factory := loadtest.NewKVStoreClientFactory()
	if err := factory.ValidateConfig(cfg); err != nil {
		b.Errorf("Unexpected error from KVStoreClientFactory.ValidateConfig(): %v", err)
	}
	client, err := factory.NewClient(cfg)
	if err != nil {
		b.Errorf("Unexpected error from KVStoreClientFactory.NewClient(): %v", err)
	}
	for n := 0; n < b.N; n++ {
		_, _ = client.GenerateTx()
	}
}

func BenchmarkKVStoreClient_GenerateTx_32b(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 32})
}

func BenchmarkKVStoreClient_GenerateTx_64b(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 64})
}

func BenchmarkKVStoreClient_GenerateTx_128b(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 128})
}

func BenchmarkKVStoreClient_GenerateTx_256b(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 256})
}

func BenchmarkKVStoreClient_GenerateTx_512b(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 512})
}

func BenchmarkKVStoreClient_GenerateTx_1kB(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 1024})
}

func BenchmarkKVStoreClient_GenerateTx_10kB(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 10240})
}

func BenchmarkKVStoreClient_GenerateTx_100kB(b *testing.B) {
	benchmarkKVStoreClient_GenerateTx(b, loadtest.Config{Size: 102400})
}

func TestKVStoreClient(t *testing.T) {
	testCases := []struct {
		config      loadtest.Config
		clientCount int
	}{
		{loadtest.Config{Size: 32, Count: 1000}, 5},
		{loadtest.Config{Size: 64, Count: 1000}, 5},
		{loadtest.Config{Size: 128, Count: 1000}, 5},
		{loadtest.Config{Size: 256, Count: 1000}, 5},
		{loadtest.Config{Size: 10240, Count: 1000}, 5},
	}
	factory := loadtest.NewKVStoreClientFactory()
	for i, tc := range testCases {
		err := factory.ValidateConfig(tc.config)
		if err != nil {
			t.Errorf("Expected config from test case %d to validate, but failed: %v", i, err)
		}

		for c := 0; c < tc.clientCount; c++ {
			client, err := factory.NewClient(tc.config)
			if err != nil {
				t.Errorf("Did not expect error in test case %d from factory.NewClient: %v", i, err)
			}
			tx, err := client.GenerateTx()
			if err != nil {
				t.Errorf("Did not expect error in test case %d from client %d's GenerateTx: %v", i, c, err)
			}
			if len(tx) != tc.config.Size {
				t.Errorf("Expected transaction from client %d in test case %d to be %d bytes, but was %d bytes", c, i, tc.config.Size, len(tx))
			}
		}
	}
}
