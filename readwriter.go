package main

/*
#include <stdlib.h>   // For free
#include <sys/mman.h> // For shm_*

#include "structs.h"
*/
import "C"

import (
	"io"
	"sync/atomic"
	"unsafe"
)

const (
	eofFlagIndex = 0
	eofFlagMask  = 0x01
)

type Buffer struct {
	block *C.shared_block_t

	Data  []byte
	Flags *[blockFlagsSize]byte
}

type ReadWriteCloser struct {
	readShared    *C.shared_mem_t
	writeShared   *C.shared_mem_t
	size          uintptr
	fullBlockSize uintptr

	closed uint64
}

func (rw *ReadWriteCloser) Close() (err error) {
	if !atomic.CompareAndSwapUint64(&rw.closed, 0, 1) {
		return nil
	}

	// finish all sends before close!

	if uintptr(unsafe.Pointer(rw.readShared)) < uintptr(unsafe.Pointer(rw.writeShared)) {
		_, err = C.munmap(unsafe.Pointer(rw.readShared), C.size_t(rw.size))
	} else {
		_, err = C.munmap(unsafe.Pointer(rw.writeShared), C.size_t(rw.size))
	}

	return
}

// Read

func (rw *ReadWriteCloser) Read(p []byte) (n int, err error) {
	buf, err := rw.GetReadBuffer()

	if err != nil {
		return 0, err
	}

	n = copy(p, buf.Data)
	isEOF := buf.Flags[eofFlagIndex]&eofFlagMask != 0

	if err = rw.SendReadBuffer(buf); err != nil {
		return n, err
	}

	if isEOF {
		return n, io.EOF
	}

	return n, nil
}

func (rw *ReadWriteCloser) WriteTo(w io.Writer) (n int64, err error) {
	for {
		buf, err := rw.GetReadBuffer()

		if err != nil {
			return 0, err
		}

		nn, err := w.Write(buf.Data)
		n += int64(nn)

		isEOF := buf.Flags[eofFlagIndex]&eofFlagMask != 0

		if putErr := rw.SendReadBuffer(buf); putErr != nil {
			return n, putErr
		}

		if err != nil || isEOF {
			return n, err
		}
	}
}

func (rw *ReadWriteCloser) GetReadBuffer() (Buffer, error) {
	if atomic.LoadUint64(&rw.closed) != 0 {
		return Buffer{}, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.readShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.readShared.read_start))

		if blockIndex > uint64(rw.readShared.block_count) {
			return Buffer{}, errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*rw.fullBlockSize))

		if blockIndex == atomic.LoadUint64((*uint64)(&rw.readShared.write_end)) {
			if err := sem_wait(&rw.readShared.sem_signal); err != nil {
				return Buffer{}, err
			}

			continue
		}

		if atomic.CompareAndSwapUint64((*uint64)(&rw.readShared.read_start), blockIndex, uint64(block.next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.flags)]byte)(unsafe.Pointer(&block.flags[0]))
	return Buffer{
		block,

		data[:block.size:rw.readShared.block_size],
		flags,
	}, nil
}

func (rw *ReadWriteCloser) SendReadBuffer(buf Buffer) error {
	if atomic.LoadUint64(&rw.closed) != 0 {
		return io.ErrClosedPipe
	}

	var block *C.shared_block_t = buf.block

	atomic.StoreUint64((*uint64)(&block.done_read), 1)

	blocks := uintptr(unsafe.Pointer(rw.readShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.readShared.read_end))

		if blockIndex > uint64(rw.readShared.block_count) {
			return errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*rw.fullBlockSize))

		if !atomic.CompareAndSwapUint64((*uint64)(&block.done_read), 1, 0) {
			return nil
		}

		atomic.CompareAndSwapUint64((*uint64)(&rw.readShared.read_end), blockIndex, uint64(block.next))

		if uint64(block.prev) == atomic.LoadUint64((*uint64)(&rw.readShared.write_start)) {
			if err := sem_post(&rw.readShared.sem_avail); err != nil {
				return err
			}
		}
	}
}

// Write

func (rw *ReadWriteCloser) Write(p []byte) (n int, err error) {
	buf, err := rw.GetWriteBuffer()

	if err != nil {
		return 0, err
	}

	n = copy(buf.Data[:cap(buf.Data)], p)
	buf.Data = buf.Data[:n]

	buf.Flags[eofFlagIndex] |= eofFlagMask

	_, err = rw.SendWriteBuffer(buf)
	return n, err
}

func (rw *ReadWriteCloser) ReadFrom(r io.Reader) (n int64, err error) {
	for {
		buf, err := rw.GetWriteBuffer()

		if err != nil {
			return 0, err
		}

		nn, err := r.Read(buf.Data[:cap(buf.Data)])
		buf.Data = buf.Data[:nn]
		n += int64(nn)

		if err == io.EOF {
			buf.Flags[eofFlagIndex] |= eofFlagMask
		} else {
			buf.Flags[eofFlagIndex] &^= eofFlagMask
		}

		if _, putErr := rw.SendWriteBuffer(buf); putErr != nil {
			return n, err
		}

		if err == io.EOF {
			return n, nil
		} else if err != nil {
			return n, err
		}
	}
}

func (rw *ReadWriteCloser) GetWriteBuffer() (Buffer, error) {
	if atomic.LoadUint64(&rw.closed) != 0 {
		return Buffer{}, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.writeShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.writeShared.write_start))

		if blockIndex > uint64(rw.writeShared.block_count) {
			return Buffer{}, errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*rw.fullBlockSize))

		if uint64(block.next) == atomic.LoadUint64((*uint64)(&rw.writeShared.read_end)) {
			if err := sem_wait(&rw.writeShared.sem_avail); err != nil {
				return Buffer{}, err
			}

			continue
		}

		if atomic.CompareAndSwapUint64((*uint64)(&rw.writeShared.write_start), blockIndex, uint64(block.next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.flags)]byte)(unsafe.Pointer(&block.flags[0]))
	return Buffer{
		block,

		data[:0:rw.writeShared.block_size],
		flags,
	}, nil
}

func (rw *ReadWriteCloser) SendWriteBuffer(buf Buffer) (n int, err error) {
	if atomic.LoadUint64(&rw.closed) != 0 {
		return 0, io.ErrClosedPipe
	}

	var block *C.shared_block_t = buf.block

	block.size = C.ulonglong(len(buf.Data))

	atomic.StoreUint64((*uint64)(&block.done_write), 1)

	blocks := uintptr(unsafe.Pointer(rw.writeShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.writeShared.write_end))

		if blockIndex > uint64(rw.writeShared.block_count) {
			return len(buf.Data), errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*rw.fullBlockSize))

		if !atomic.CompareAndSwapUint64((*uint64)(&block.done_write), 1, 0) {
			return len(buf.Data), nil
		}

		atomic.CompareAndSwapUint64((*uint64)(&rw.writeShared.write_end), blockIndex, uint64(block.next))

		if blockIndex == atomic.LoadUint64((*uint64)(&rw.writeShared.read_start)) {
			if err := sem_post(&rw.writeShared.sem_signal); err != nil {
				return len(buf.Data), err
			}
		}
	}
}
