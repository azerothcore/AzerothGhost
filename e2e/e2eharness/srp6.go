package e2eharness

import (
	"crypto/rand"
	"crypto/sha1"
	"math/big"
)

// ComputeSRP6 returns salt and verifier matching ToCloud9 authserver / AzerothCore:
// v = g^x mod N with x = SHA1(salt | SHA1(username:password)) as little-endian int.
func ComputeSRP6(username, password string) (salt, verifier []byte) {
	salt = make([]byte, 32)
	_, _ = rand.Read(salt)

	inner := sha1.Sum([]byte(username + ":" + password))
	xDigest := sha1.Sum(append(append([]byte{}, salt...), inner[:]...))
	x := leToBig(xDigest[:])

	Nle := []byte{
		0xB7, 0x9B, 0x3E, 0x2A, 0x87, 0x82, 0x3C, 0xAB,
		0x8F, 0x5E, 0xBF, 0xBF, 0x8E, 0xB1, 0x01, 0x08,
		0x53, 0x50, 0x06, 0x29, 0x8B, 0x5B, 0xAD, 0xBD,
		0x5B, 0x53, 0xE1, 0x89, 0x5E, 0x64, 0x4B, 0x89,
	}
	N := leToBig(Nle)
	g := big.NewInt(7)
	v := new(big.Int).Exp(g, x, N)
	return salt, bigToLE(v, 32)
}

func leToBig(b []byte) *big.Int {
	be := make([]byte, len(b))
	for i := range b {
		be[len(b)-1-i] = b[i]
	}
	return new(big.Int).SetBytes(be)
}

func bigToLE(i *big.Int, size int) []byte {
	be := i.Bytes()
	out := make([]byte, size)
	for j := 0; j < len(be) && j < size; j++ {
		out[j] = be[len(be)-1-j]
	}
	return out
}
