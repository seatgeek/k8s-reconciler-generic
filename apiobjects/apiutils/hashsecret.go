package apiutils

import (
	"crypto/sha256"
	"encoding/base32"
	"sort"
)

var enc32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func HashSecretData(data map[string][]byte) string {
	ps := make([]byte, 0, 32*len(data))
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		hash := sha256.Sum256(append([]byte(key+" "), data[key]...))
		ps = append(ps, hash[:]...)
	}
	root := sha256.Sum256(ps)
	return enc32.EncodeToString(root[:])
}
