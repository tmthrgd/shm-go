// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

// +build !linux !386,!amd64

package shm

/*
#include <stdint.h>    // For (u)int*_t
#include <semaphore.h> // For sem_*

typedef struct {
	uint32_t Next;
	uint32_t Prev;

	uint32_t DoneRead;
	uint32_t DoneWrite;

	uint64_t Size;

	uint8_t Flags[(0x40-(2*2*sizeof(uint32_t)+sizeof(uint64_t))&0x3f)&0x3f];

	uint8_t Data[];
} shared_block_t;

typedef struct {
	uint32_t Version;
	uint32_t __padding0;

	uint32_t BlockCount;
	uint32_t __padding1;

	uint64_t BlockSize;

	uint32_t ReadStart;
	uint32_t ReadEnd;

	uint32_t WriteStart;
	uint32_t WriteEnd;

	sem_t SemSignal;
	sem_t SemAvail;

	uint32_t Flags[((0x40-(4*2*sizeof(uint32_t)+sizeof(uint64_t)+2*sizeof(sem_t))&0x3f)&0x3f)/4];

	shared_block_t Blocks[];
} shared_mem_t;
*/
import "C"

type sharedBlock C.shared_block_t

type sharedMem C.shared_mem_t

const (
	sharedHeaderSize = C.sizeof_shared_mem_t
	sharedFlagsSize  = len(sharedMem{}.Flags)
	blockHeaderSize  = C.sizeof_shared_block_t
	blockFlagsSize   = len(sharedBlock{}.Flags)

	version = uint32((^uint(0)>>32)&0x80000000) | 0x00000001
)
