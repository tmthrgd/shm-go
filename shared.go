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
	uint64_t Next;
	uint64_t Prev;

	uint64_t DoneRead;
	uint64_t DoneWrite;

	uint64_t Size;

	uint8_t Flags[(0x40-((2*2+1)*sizeof(uint64_t))&0x3f)&0x3f];

	uint8_t Data[];
} shared_block_t;

typedef struct {
	uint64_t Version;

	uint64_t BlockCount;
	uint64_t BlockSize;

	uint64_t ReadStart;
	uint64_t ReadEnd;

	uint64_t WriteStart;
	uint64_t WriteEnd;

	sem_t SemSignal;
	sem_t SemAvail;

	uint8_t __padding[(0x40-((1+3*2)*sizeof(uint64_t)+2*sizeof(sem_t))&0x3f)&0x3f];

	shared_block_t Blocks[];
} shared_mem_t;
*/
import "C"

type sharedBlock C.shared_block_t

type sharedMem C.shared_mem_t

const (
	sharedHeaderSize = C.sizeof_shared_mem_t
	blockHeaderSize  = C.sizeof_shared_block_t
	blockFlagsSize   = len(sharedBlock{}.Flags)

	version = uint64((^uint(0)>>32)&0x80000000)<<32 | 0x00000001
)
