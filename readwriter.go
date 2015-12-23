package main

/*
#include <stdlib.h>   // For free
#include <sys/mman.h> // For shm_*

#include "structs.h"
*/
import "C"

import (
	"io"
	"sync"
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

type ReadWriter struct {
	shared *C.shared_mem_t
	len    uintptr
	name   string

	mut      sync.Mutex
	closed   int64
	unlinked bool
}

func (rw *ReadWriter) Close() error {
	rw.mut.Lock()
	defer rw.mut.Unlock()

	if atomic.LoadInt64(&rw.closed) != 0 {
		return nil
	}

	atomic.StoreInt64(&rw.closed, 1)

	_, err := C.munmap(unsafe.Pointer(rw.shared), C.size_t(rw.len))
	return err
}

func (rw *ReadWriter) Unlink() error {
	if len(rw.name) == 0 {
		return ErrNotOwner
	}

	rw.mut.Lock()
	defer rw.mut.Unlock()

	if rw.unlinked {
		return nil
	}

	if atomic.LoadInt64(&rw.closed) == 0 {
		atomic.StoreInt64(&rw.closed, 1)

		if _, err := C.munmap(unsafe.Pointer(rw.shared), C.size_t(rw.len)); err != nil {
			return err
		}
	}

	rw.unlinked = true

	if err := sem_destroy(&rw.shared.sem_signal); err != nil {
		return err
	}

	if err := sem_destroy(&rw.shared.sem_avail); err != nil {
		return err
	}

	nameC := C.CString(rw.name)
	_, err := C.shm_unlink(nameC)
	C.free(unsafe.Pointer(nameC))
	return err
}

// Read

func (rw *ReadWriter) Read(p []byte) (n int, err error) {
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

func (rw *ReadWriter) WriteTo(w io.Writer) (n int64, err error) {
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

func (rw *ReadWriter) GetReadBuffer() (*Buffer, error) {
	if atomic.LoadInt64(&rw.closed) != 0 {
		return nil, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.read_start))
		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if int64(block.next) == atomic.LoadInt64((*int64)(&rw.shared.write_end)) {
			if err := sem_wait(&rw.shared.sem_signal); err != nil {
				return nil, err
			}

			continue
		}

		if atomic.CompareAndSwapInt64((*int64)(&rw.shared.read_start), blockIndex, int64(block.next)) {
			break
		}
	}

	size := block.size

	if size > rw.shared.block_size {
		size = rw.shared.block_size
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.flags)]byte)(unsafe.Pointer(&block.flags[0]))
	return &Buffer{
		block,
		data[:size:size],
		flags[:],
	}, nil
}

func (rw *ReadWriter) SendReadBuffer(buf *Buffer) error {
	var block *C.shared_block_t = buf.block

	atomic.StoreInt64((*int64)(&block.done_read), 1)

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.read_end))
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

func (rw *ReadWriter) Write(p []byte) (n int, err error) {
	buf, err := rw.GetWriteBuffer()

	if err != nil {
		return 0, err
	}

	n = copy(buf.Data[:cap(buf.Data)], p)
	buf.Data = buf.Data[:n]

	if len(p) == 0 {
		buf.Flags[eofFlagIndex] |= eofFlagMask
	} else {
		buf.Flags[eofFlagIndex] &^= eofFlagMask
	}

	_, err = rw.SendWriteBuffer(buf)
	return n, err
}

func (rw *ReadWriter) ReadFrom(r io.Reader) (n int64, err error) {
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

func (rw *ReadWriter) GetWriteBuffer() (*Buffer, error) {
	if atomic.LoadInt64(&rw.closed) != 0 {
		return nil, io.ErrClosedPipe
	}

	var block *C.shared_block_t

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.write_start))
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

func (rw *ReadWriter) SendWriteBuffer(buf *Buffer) (n int, err error) {
	var block *C.shared_block_t = buf.block

	block.size = C.longlong(len(buf.Data))

	atomic.StoreInt64((*int64)(&block.done_write), 1)

	blocks := uintptr(unsafe.Pointer(rw.shared)) + sharedHeaderSize
	fullBlockSize := blockHeaderSize + uintptr(rw.shared.block_size)

	for {
		blockIndex := atomic.LoadInt64((*int64)(&rw.shared.write_end))
		block = (*C.shared_block_t)(unsafe.Pointer(blocks + uintptr(blockIndex)*fullBlockSize))

		if !atomic.CompareAndSwapInt64((*int64)(&block.done_write), 1, 0) {
			return len(buf.Data), nil
		}

		atomic.CompareAndSwapInt64((*int64)(&rw.shared.write_end), blockIndex, int64(block.next))

		if int64(block.prev) == atomic.LoadInt64((*int64)(&rw.shared.read_start)) {
			if err := sem_post(&rw.shared.sem_signal); err != nil {
				return len(buf.Data), err
			}
		}
	}
}
