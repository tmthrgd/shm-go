package shm

import "errors"

var (
	ErrNotMultipleOf64   = errors.New("blockSize is not a multiple of 64")
	ErrInvalidBlockIndex = errors.New("invalid block index")
)
