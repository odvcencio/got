package object

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// HashBytes computes the raw SHA-256 hash of data and returns it as a
// lowercase hex-encoded Hash.
func HashBytes(data []byte) Hash {
	sum := sha256.Sum256(data)
	return Hash(hex.EncodeToString(sum[:]))
}

// HashObject computes the SHA-256 of the envelope "type len\0content",
// mirroring Git's object hashing but with SHA-256.
func HashObject(objType ObjectType, data []byte) Hash {
	header := fmt.Sprintf("%s %d\x00", objType, len(data))
	h := sha256.New()
	h.Write([]byte(header))
	h.Write(data)
	return Hash(hex.EncodeToString(h.Sum(nil)))
}
