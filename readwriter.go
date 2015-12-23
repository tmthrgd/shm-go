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

type Reader interface {
	io.Reader
	io.WriterTo

	GetReadBuffer() (*Buffer, error)
	SendReadBuffer(buf *Buffer) error
}

type ReadCloser interface {
	Reader
	io.Closer
}

type Writer interface {
	io.Writer
	io.ReaderFrom

	GetWriteBuffer() (*Buffer, error)
	SendWriteBuffer(buf *Buffer) (int, error)
}

type WriteCloser interface {
	Writer
	io.Closer
}

type ReadWriter interface {
	Reader
	Writer
}

type ReadWriteCloser interface {
	ReadWriter
	io.Closer
}

type readWriter struct {
	shared *C.shared_mem_t
	size   uintptr

	closed *int64
}

func (rw *readWriter) Close() error {
	if rw.size == 0 {
		return ErrNotCloser
	}

	if atomic.CompareAndSwapInt64(rw.closed, 0, 1) {
		return nil
	}

	// finish all sends before close!

	_, err := C.munmap(unsafe.Pointer(rw.shared), C.size_t(rw.size))
	return err
}

// Read

func (rw *readWriter) Read(p []byte) (n int, err error) {
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

func (rw *readWriter) WriteTo(w io.Writer) (n int64, err error) {
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

func (rw *readWriter) GetReadBuffer() (*Buffer, error) {
	if atomic.LoadInt64(rw.closed) != 0 {
		return nil, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.read_start))

		if blockIndex < 0 || blockIndex > int64(rw.shared.block_count) {
			return nil, errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if blockIndex == atomic.LoadInt64((*int64)(&rw.shared.write_end)) {
			if err := sem_wait(&rw.shared.sem_signal); err != nil {
				return nil, err
			}

			continue
		}

		if atomic.CompareAndSwapInt64((*int64)(&rw.shared.read_start), blockIndex, int64(block.next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.flags)]byte)(unsafe.Pointer(&block.flags[0]))
	return &Buffer{
		block,

		data[:block.size:rw.shared.block_size],
		flags[:],
	}, nil
}

func (rw *readWriter) SendReadBuffer(buf *Buffer) error {
	if atomic.LoadInt64(rw.closed) != 0 {
		return io.ErrClosedPipe
	}

	var block *C.shared_block_t = buf.block

	atomic.StoreInt64((*int64)(&block.done_read), 1)

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.read_end))

		if blockIndex < 0 || blockIndex > int64(rw.shared.block_count) {
			return errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if !atomic.CompareAndSwapInt64((*int64)(&block.done_read), 1, 0) {
			return nil
		}

		atomic.CompareAndSwapInt64((*int64)(&rw.shared.read_end), blockIndex, int64(block.next))

		if int64(block.prev) == atomic.LoadInt64((*int64)(&rw.shared.write_start)) {
			if err := sem_post(&rw.shared.sem_avail); err != nil {
				return err
			}
		}
	}
}

// Write

func (rw *readWriter) Write(p []byte) (n int, err error) {
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

func (rw *readWriter) ReadFrom(r io.Reader) (n int64, err error) {
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

func (rw *readWriter) GetWriteBuffer() (*Buffer, error) {
	if atomic.LoadInt64(rw.closed) != 0 {
		return nil, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.write_start))

		if blockIndex < 0 || blockIndex > int64(rw.shared.block_count) {
			return nil, errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if int64(block.next) == atomic.LoadInt64((*int64)(&rw.shared.read_end)) {
			if err := sem_wait(&rw.shared.sem_avail); err != nil {
				return nil, err
			}

			continue
		}

		if atomic.CompareAndSwapInt64((*int64)(&rw.shared.write_start), blockIndex, int64(block.next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.flags)]byte)(unsafe.Pointer(&block.flags[0]))
	return &Buffer{
		block,

		data[:0:rw.shared.block_size],
		flags[:],
	}, nil
}

func (rw *readWriter) SendWriteBuffer(buf *Buffer) (n int, err error) {
	if atomic.LoadInt64(rw.closed) != 0 {
		return 0, io.ErrClosedPipe
	}

	var block *C.shared_block_t = buf.block

	block.size = C.longlong(len(buf.Data))

	atomic.StoreInt64((*int64)(&block.done_write), 1)

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.write_end))

		if blockIndex < 0 || blockIndex > int64(rw.shared.block_count) {
			return len(buf.Data), errInvalidBlockIndex
		}

		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if !atomic.CompareAndSwapInt64((*int64)(&block.done_write), 1, 0) {
			return len(buf.Data), nil
		}

		atomic.CompareAndSwapInt64((*int64)(&rw.shared.write_end), blockIndex, int64(block.next))

		//if int64(block.prev) == atomic.LoadInt64((*int64)(&rw.shared.read_start)) {
		if blockIndex == atomic.LoadInt64((*int64)(&rw.shared.read_start)) {
			if err := sem_post(&rw.shared.sem_signal); err != nil {
				return len(buf.Data), err
			}
		}
	}
}
