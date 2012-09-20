package main

import (
	"testing"
)

func BenchmarkRandomise(b *testing.B) {
	b.StopTimer()
	random := NewRandom()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		random.Randomise()
	}
}
