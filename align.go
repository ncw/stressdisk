// Alignment blocks of memory
package main

import (
	"log"
	"unsafe"
)

// alignment returns alignment of the block in memory
// with reference to AlignSize
func alignment(block []byte, AlignSize int) int {
	return int(uintptr(unsafe.Pointer(&block[0])) & uintptr(AlignSize-1))
}

// makeAlignedBlock returns []byte of size BlockSize aligned to a
// multiple of AlignSize in memory (must be power of two)
func makeAlignedBlock(BlockSize int) []byte {
	block := make([]byte, BlockSize+AlignSize)
	if AlignSize == 0 {
		return block
	}
	a := alignment(block, AlignSize)
	offset := 0
	if a != 0 {
		offset = AlignSize - a
	}
	block = block[offset : offset+BlockSize]
	a = alignment(block, AlignSize)
	if a != 0 {
		log.Fatal("Failed to align block")
	}
	return block
}
