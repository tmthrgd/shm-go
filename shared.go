// +build !linux !386,!amd64

package shm

/*
#include <semaphore.h> // For sem_*

typedef struct {
	unsigned long long Next;
	unsigned long long Prev;

	unsigned long long DoneRead;
	unsigned long long DoneWrite;

	unsigned long long Size;

	unsigned char Flags[(0x40-((2*2+1)*sizeof(long long))&0x3f)&0x3f];

	unsigned char Data[];
} shared_block_t;

typedef struct {
	unsigned long long BlockCount;
	unsigned long long BlockSize;

	unsigned long long ReadStart;
	unsigned long long ReadEnd;

	unsigned long long WriteStart;
	unsigned long long WriteEnd;

	sem_t SemSignal;
	sem_t SemAvail;

	unsigned char Flags[(0x40-(3*2*sizeof(long long)+2*sizeof(sem_t))&0x3f)&0x3f];

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
	headerFlagsSize  = len(sharedMem{}.Flags)
)
