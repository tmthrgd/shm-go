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
	Flags []byte
}

type ReadWriteCloser struct {
	read_shared  *C.shared_mem_t
	write_shared *C.shared_mem_t
	size         uintptr

	closed int64
}

func (rw *ReadWriteCloser) Close() (err error) {
	if !atomic.CompareAndSwapInt64(&rw.closed, 0, 1) {
		return nil
	}

	// finish all sends before close!

	if uintptr(unsafe.Pointer(rw.read_shared)) < uintptr(unsafe.Pointer(rw.write_shared)) {
		_, err = C.munmap(unsafe.Pointer(rw.read_shared), C.size_t(rw.size))
	} else {
		_, err = C.munmap(unsafe.Pointer(rw.write_shared), C.size_t(rw.size))
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

func (rw *ReadWriteCloser) GetReadBuffer() (*Buffer, error) {
	if atomic.LoadInt64(&rw.closed) != 0 {
		return nil, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.read_shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.read_shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.read_shared.read_start))

		if blockIndex < 0 || blockIndex > int64(rw.read_shared.block_count) {
			return nil, errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if blockIndex == atomic.LoadInt64((*int64)(&rw.read_shared.write_end)) {
			if err := sem_wait(&rw.read_shared.sem_signal); err != nil {
				return nil, err
			}

			continue
		}

		if atomic.CompareAndSwapInt64((*int64)(&rw.read_shared.read_start), blockIndex, int64(block.next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.flags)]byte)(unsafe.Pointer(&block.flags[0]))
	return &Buffer{
		block,

		data[:block.size:rw.read_shared.block_size],
		flags[:],
	}, nil
}

func (rw *ReadWriteCloser) SendReadBuffer(buf *Buffer) error {
	if atomic.LoadInt64(&rw.closed) != 0 {
		return io.ErrClosedPipe
	}

	var block *C.shared_block_t = buf.block

	atomic.StoreInt64((*int64)(&block.done_read), 1)

	blocks := uintptr(unsafe.Pointer(rw.read_shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.read_shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.read_shared.read_end))

		if blockIndex < 0 || blockIndex > int64(rw.read_shared.block_count) {
			return errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if !atomic.CompareAndSwapInt64((*int64)(&block.done_read), 1, 0) {
			return nil
		}

		atomic.CompareAndSwapInt64((*int64)(&rw.read_shared.read_end), blockIndex, int64(block.next))

		if int64(block.prev) == atomic.LoadInt64((*int64)(&rw.read_shared.write_start)) {
			if err := sem_post(&rw.read_shared.sem_avail); err != nil {
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

func (rw *ReadWriteCloser) GetWriteBuffer() (*Buffer, error) {
	if atomic.LoadInt64(&rw.closed) != 0 {
		return nil, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.write_shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.write_shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.write_shared.write_start))

		if blockIndex < 0 || blockIndex > int64(rw.write_shared.block_count) {
			return nil, errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if int64(block.next) == atomic.LoadInt64((*int64)(&rw.write_shared.read_end)) {
			if err := sem_wait(&rw.write_shared.sem_avail); err != nil {
				return nil, err
			}

			continue
		}

		if atomic.CompareAndSwapInt64((*int64)(&rw.write_shared.write_start), blockIndex, int64(block.next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.flags)]byte)(unsafe.Pointer(&block.flags[0]))
	return &Buffer{
		block,

		data[:0:rw.write_shared.block_size],
		flags[:],
	}, nil
}

func (rw *ReadWriteCloser) SendWriteBuffer(buf *Buffer) (n int, err error) {
	if atomic.LoadInt64(&rw.closed) != 0 {
		return 0, io.ErrClosedPipe
	}

	var block *C.shared_block_t = buf.block

	block.size = C.longlong(len(buf.Data))

	atomic.StoreInt64((*int64)(&block.done_write), 1)

	blocks := uintptr(unsafe.Pointer(rw.write_shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.write_shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.write_shared.write_end))

		if blockIndex < 0 || blockIndex > int64(rw.write_shared.block_count) {
			return len(buf.Data), errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if !atomic.CompareAndSwapInt64((*int64)(&block.done_write), 1, 0) {
			return len(buf.Data), nil
		}

		atomic.CompareAndSwapInt64((*int64)(&rw.write_shared.write_end), blockIndex, int64(block.next))

		if blockIndex == atomic.LoadInt64((*int64)(&rw.write_shared.read_start)) {
			if err := sem_post(&rw.write_shared.sem_signal); err != nil {
				return len(buf.Data), err
			}
		}
	}
}
