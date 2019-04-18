package clients

import (
	"math/rand"
	"time"

	cmn "github.com/tendermint/tendermint/libs/common"
)

// MakeTxKV returns a text transaction, along with expected key, value pair
func MakeTxKV() ([]byte, []byte, []byte) {
	k := []byte(cmn.RandStr(8))
	v := []byte(cmn.RandStr(8))
	return k, v, append(k, append([]byte("="), v...)...)
}

// RandomSleep will sleep for a random period of between the given minimum and
// maximum times.
func RandomSleep(minSleep, maxSleep time.Duration) {
	if minSleep == maxSleep {
		time.Sleep(minSleep)
	} else {
		time.Sleep(minSleep + time.Duration(rand.Int63n(int64(maxSleep)-int64(minSleep))))
	}
}

// TimeFn will execute the given function and return the time it took to execute
// the function as well as whatever the function returns (error or otherwise).
func TimeFn(fn func() error) (time.Duration, error) {
	startTime := time.Now()
	err := fn()
	return time.Since(startTime), err
}
