// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package shm

import (
	"golang.org/x/sys/unix"
	"os"
	"sync/atomic"
	"unsafe"

	"github.com/tmthrgd/go-sem"
)

func CreateSimplex(name string, perm os.FileMode, blockCount, blockSize int) (*ReadWriteCloser, error) {
	if blockSize&0x3f != 0 {
		return nil, ErrNotMultipleOf64
	}

	file, err := shmOpen(name, unix.O_CREAT|unix.O_EXCL|unix.O_TRUNC|unix.O_RDWR, perm)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}

	defer file.Close()

	fullBlockSize := blockHeaderSize + uint64(blockSize)
	size := sharedHeaderSize + fullBlockSize*uint64(blockCount)

	if err = file.Truncate(int64(size)); err != nil {
		return nil, err
	}

	data, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	shared := (*sharedMem)(unsafe.Pointer(&data[0]))

	/*
	 * memset already set:
	 *	shared.ReadStart, shared.ReadEnd = 0, 0
	 *	shared.WriteStart, shared.WriteEnd = 0, 0
	 *	shared.block[i].Size = 0
	 *	shared.block[i].DoneRead, shared.block[i].DoneWrite = 0, 0
	 */
	*(*uint32)(&shared.BlockCount), *(*uint64)(&shared.BlockSize) = uint32(blockCount), uint64(blockSize)

	if err = ((*sem.Semaphore)(&shared.SemSignal)).Init(0); err != nil {
		return nil, err
	}

	if err = ((*sem.Semaphore)(&shared.SemAvail)).Init(0); err != nil {
		return nil, err
	}

	for i := uint32(0); i < uint32(blockCount); i++ {
		block := (*sharedBlock)(unsafe.Pointer(&data[sharedHeaderSize+uint64(i)*fullBlockSize]))

		switch i {
		case 0:
			block.Next, *(*uint32)(&block.Prev) = 1, uint32(blockCount-1)
		case uint32(blockCount - 1):
			block.Next, *(*uint32)(&block.Prev) = 0, uint32(blockCount-2)
		default:
			*(*uint32)(&block.Next), *(*uint32)(&block.Prev) = i+1, i-1
		}
	}

	atomic.StoreUint32((*uint32)(&shared.Version), version)

	return &ReadWriteCloser{
		name: name,

		data:          data,
		readShared:    shared,
		writeShared:   shared,
		size:          size,
		fullBlockSize: fullBlockSize,
	}, nil
}

func CreateDuplex(name string, perm os.FileMode, blockCount, blockSize int) (*ReadWriteCloser, error) {
	if blockSize&0x3f != 0 {
		return nil, ErrNotMultipleOf64
	}

	file, err := shmOpen(name, unix.O_CREAT|unix.O_EXCL|unix.O_TRUNC|unix.O_RDWR, perm)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}

	defer file.Close()

	fullBlockSize := blockHeaderSize + uint64(blockSize)
	sharedSize := sharedHeaderSize + fullBlockSize*uint64(blockCount)
	size := 2 * sharedSize

	if err = file.Truncate(int64(size)); err != nil {
		return nil, err
	}

	data, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	for i := uint64(0); i < 2; i++ {
		shared := (*sharedMem)(unsafe.Pointer(&data[i*sharedSize]))

		/*
		 * memset already set:
		 *	shared.ReadStart, shared.ReadEnd = 0, 0
		 *	shared.WriteStart, shared.WriteEnd = 0, 0
		 *	shared.Blocks[i].Size = 0
		 *	shared.Blocks[i].DoneRead, shared.Blocks[i].DoneWrite = 0, 0
		 */
		*(*uint32)(&shared.BlockCount), *(*uint64)(&shared.BlockSize) = uint32(blockCount), uint64(blockSize)

		if err = ((*sem.Semaphore)(&shared.SemSignal)).Init(0); err != nil {
			return nil, err
		}

		if err = ((*sem.Semaphore)(&shared.SemAvail)).Init(0); err != nil {
			return nil, err
		}

		for j := uint32(0); j < uint32(blockCount); j++ {
			block := (*sharedBlock)(unsafe.Pointer(&data[i*sharedSize+sharedHeaderSize+uint64(j)*fullBlockSize]))

			switch j {
			case 0:
				block.Next, *(*uint32)(&block.Prev) = 1, uint32(blockCount-1)
			case uint32(blockCount - 1):
				block.Next, *(*uint32)(&block.Prev) = 0, uint32(blockCount-2)
			default:
				*(*uint32)(&block.Next), *(*uint32)(&block.Prev) = j+1, j-1
			}
		}

		atomic.StoreUint32((*uint32)(&shared.Version), version)
	}

	return &ReadWriteCloser{
		name: name,

		data:          data,
		readShared:    (*sharedMem)(unsafe.Pointer(&data[0])),
		writeShared:   (*sharedMem)(unsafe.Pointer(&data[sharedSize])),
		size:          size,
		fullBlockSize: fullBlockSize,
	}, nil
}
