// Copyright 2016 Tom Thorogood. All rights reserved.
// Use of this source code is governed by a
// Modified BSD License license that can be found in
// the LICENSE file.

package shm

import (
	"golang.org/x/sys/unix"
	"io"
	"sync/atomic"
	"unsafe"

	"github.com/tmthrgd/go-sem"
)

const (
	eofFlagIndex = 0
	eofFlagMask  = 0x01
)

type Buffer struct {
	block *sharedBlock
	write bool

	Data  []byte
	Flags *[blockFlagsSize]byte
}

type ReadWriteCloser struct {
	data          []byte
	readShared    *sharedMem
	writeShared   *sharedMem
	size          uint64
	fullBlockSize uint64

	closed uint64
}

func (rw *ReadWriteCloser) Close() error {
	if !atomic.CompareAndSwapUint64(&rw.closed, 0, 1) {
		return nil
	}

	// finish all sends before close!

	return unix.Munmap(rw.data)
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
			return n, err
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

	var block *sharedBlock

	blocks := uintptr(unsafe.Pointer(rw.readShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.readShared.ReadStart))
		if blockIndex > uint64(rw.readShared.BlockCount) {
			return Buffer{}, ErrInvalidSharedMemory
		}

		block = (*sharedBlock)(unsafe.Pointer(blocks + uintptr(blockIndex*rw.fullBlockSize)))

		if blockIndex == atomic.LoadUint64((*uint64)(&rw.readShared.WriteEnd)) {
			if err := ((*sem.Semaphore)(&rw.readShared.SemSignal)).Wait(); err != nil {
				return Buffer{}, err
			}

			continue
		}

		if atomic.CompareAndSwapUint64((*uint64)(&rw.readShared.ReadStart), blockIndex, uint64(block.Next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.Flags)]byte)(unsafe.Pointer(&block.Flags[0]))
	return Buffer{
		block: block,

		Data:  data[:block.Size:rw.readShared.BlockSize],
		Flags: flags,
	}, nil
}

func (rw *ReadWriteCloser) SendReadBuffer(buf Buffer) error {
	if atomic.LoadUint64(&rw.closed) != 0 {
		return io.ErrClosedPipe
	}

	if buf.write {
		return ErrInvalidBuffer
	}

	var block *sharedBlock = buf.block

	atomic.StoreUint64((*uint64)(&block.DoneRead), 1)

	blocks := uintptr(unsafe.Pointer(rw.readShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.readShared.ReadEnd))
		if blockIndex > uint64(rw.readShared.BlockCount) {
			return ErrInvalidSharedMemory
		}

		block = (*sharedBlock)(unsafe.Pointer(blocks + uintptr(blockIndex*rw.fullBlockSize)))

		if !atomic.CompareAndSwapUint64((*uint64)(&block.DoneRead), 1, 0) {
			return nil
		}

		atomic.CompareAndSwapUint64((*uint64)(&rw.readShared.ReadEnd), blockIndex, uint64(block.Next))

		if uint64(block.Prev) == atomic.LoadUint64((*uint64)(&rw.readShared.WriteStart)) {
			if err := ((*sem.Semaphore)(&rw.readShared.SemAvail)).Post(); err != nil {
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
			return n, err
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

	var block *sharedBlock

	blocks := uintptr(unsafe.Pointer(rw.writeShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.writeShared.WriteStart))
		if blockIndex > uint64(rw.writeShared.BlockCount) {
			return Buffer{}, ErrInvalidSharedMemory
		}

		block = (*sharedBlock)(unsafe.Pointer(blocks + uintptr(blockIndex*rw.fullBlockSize)))

		if uint64(block.Next) == atomic.LoadUint64((*uint64)(&rw.writeShared.ReadEnd)) {
			if err := ((*sem.Semaphore)(&rw.writeShared.SemAvail)).Wait(); err != nil {
				return Buffer{}, err
			}

			continue
		}

		if atomic.CompareAndSwapUint64((*uint64)(&rw.writeShared.WriteStart), blockIndex, uint64(block.Next)) {
			break
		}
	}

	data := (*[1 << 30]byte)(unsafe.Pointer(uintptr(unsafe.Pointer(block)) + blockHeaderSize))
	flags := (*[len(block.Flags)]byte)(unsafe.Pointer(&block.Flags[0]))
	return Buffer{
		block: block,
		write: true,

		Data:  data[:0:rw.writeShared.BlockSize],
		Flags: flags,
	}, nil
}

func (rw *ReadWriteCloser) SendWriteBuffer(buf Buffer) (n int, err error) {
	if atomic.LoadUint64(&rw.closed) != 0 {
		return 0, io.ErrClosedPipe
	}

	if !buf.write {
		return 0, ErrInvalidBuffer
	}

	var block *sharedBlock = buf.block

	*(*uint64)(&block.Size) = uint64(len(buf.Data))

	atomic.StoreUint64((*uint64)(&block.DoneWrite), 1)

	blocks := uintptr(unsafe.Pointer(rw.writeShared)) + sharedHeaderSize

	for {
		blockIndex := atomic.LoadUint64((*uint64)(&rw.writeShared.WriteEnd))
		if blockIndex > uint64(rw.writeShared.BlockCount) {
			return len(buf.Data), ErrInvalidSharedMemory
		}

		block = (*sharedBlock)(unsafe.Pointer(blocks + uintptr(blockIndex*rw.fullBlockSize)))

		if !atomic.CompareAndSwapUint64((*uint64)(&block.DoneWrite), 1, 0) {
			return len(buf.Data), nil
		}

		atomic.CompareAndSwapUint64((*uint64)(&rw.writeShared.WriteEnd), blockIndex, uint64(block.Next))

		if blockIndex == atomic.LoadUint64((*uint64)(&rw.writeShared.ReadStart)) {
			if err := ((*sem.Semaphore)(&rw.writeShared.SemSignal)).Post(); err != nil {
				return len(buf.Data), err
			}
		}
	}
}
