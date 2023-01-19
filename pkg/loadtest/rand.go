package loadtest

import (
	"crypto/rand"
	"fmt"
)

const (
	strChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" // 62 characters
)

// Generates a cryptographically random string of the given length. The global
// strChars constant is used as the character set.
func randStr(length int) string {
	if length <= 0 {
		return ""
	}

	chars := make([]byte, length)
	n, err := rand.Read(chars)
	if err != nil {
		panic(err)
	}
	if n != length {
		panic(fmt.Sprintf("expected to read %d random chars, but read %d instead", length, n))
	}
	// Select all chars from our character set.
	for i := range chars {
		chars[i] = strChars[chars[i]%byte(len(strChars))]
	}

	return string(chars)
}
