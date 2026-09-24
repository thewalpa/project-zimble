// Package random provides deterministic, versioned pseudorandom streams.
//
// The algorithms are implemented here rather than taken from math/rand so
// that their output is fixed by this repository, not by the Go release.
// Changing any output of this package changes generated worlds and requires
// bumping Version.
package random

import (
	"hash/fnv"
	"math"
)

// Version identifies the generator and seed-derivation algorithms.
const Version = 1

// Seed is an explicit world seed supplied by the caller.
type Seed uint64

const golden = 0x9e3779b97f4a7c15

// Stream is a SplitMix64 generator. It is not safe for concurrent use.
type Stream struct {
	state uint64
}

// NewStream returns a stream whose first output matches the SplitMix64
// reference implementation seeded with state.
func NewStream(state uint64) *Stream {
	return &Stream{state: state}
}

// Derive returns an independent stream for a subsystem and stable context,
// for example (worldSeed, "worldgen/player", generatorVersion, playerID).
// The domain string is hashed with FNV-1a 64, never the runtime's map hash.
func Derive(seed Seed, domain string, keys ...uint64) *Stream {
	h := fnv.New64a()
	h.Write([]byte(domain))
	state := mix(uint64(seed) + golden)
	state = mix(state ^ h.Sum64())
	for _, k := range keys {
		state = mix(state ^ (k + golden))
	}
	return NewStream(state)
}

// State returns the generator state, so a stream can be handed to a
// component that owns its own generator. NewStream(s.State()) continues
// the same sequence.
func (s *Stream) State() uint64 { return s.state }

// Uint64 returns the next 64 pseudorandom bits.
func (s *Stream) Uint64() uint64 {
	s.state += golden
	return mix(s.state)
}

// IntN returns a uniform value in [0, n). It panics if n <= 0.
// Rejection sampling removes modulo bias.
func (s *Stream) IntN(n int) int {
	if n <= 0 {
		panic("random: IntN called with n <= 0")
	}
	bound := uint64(n)
	limit := math.MaxUint64 - math.MaxUint64%bound
	for {
		if v := s.Uint64(); v < limit {
			return int(v % bound)
		}
	}
}

// IntRange returns a uniform value in [lo, hi]. It panics if hi < lo.
func (s *Stream) IntRange(lo, hi int) int {
	if hi < lo {
		panic("random: IntRange called with hi < lo")
	}
	return lo + s.IntN(hi-lo+1)
}

// Perm returns a pseudorandom permutation of [0, n) using Fisher-Yates.
func (s *Stream) Perm(n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := s.IntN(i + 1)
		p[i], p[j] = p[j], p[i]
	}
	return p
}

func mix(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}
