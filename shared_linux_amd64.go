// Created by cgo -godefs - DO NOT EDIT
// cgo -godefs shared.go

package shm

type sharedBlock struct {
	Next      uint32
	Prev      uint32
	DoneRead  uint32
	DoneWrite uint32
	Size      uint64
	Flags     [40]uint8
}

type sharedMem struct {
	Version     uint32
	X__padding0 uint32
	BlockCount  uint32
	X__padding1 uint32
	BlockSize   uint64
	ReadStart   uint32
	ReadEnd     uint32
	WriteStart  uint32
	WriteEnd    uint32
	SemSignal   [32]byte
	SemAvail    [32]byte
	Flags       [6]uint32
}

const (
	sharedHeaderSize = 0x80
	sharedFlagsSize  = len(sharedMem{}.Flags)
	blockHeaderSize  = 0x40
	blockFlagsSize   = len(sharedBlock{}.Flags)

	version = uint32((^uint(0)>>32)&0x80000000) | 0x00000001
)
