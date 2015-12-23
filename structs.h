#include <semaphore.h> // For sem_*

typedef struct {
	long long next;
	long long prev;

	volatile long long done_read;
	volatile long long done_write;

	volatile long long size;

	volatile char flags[(0x40-((2*2+1)*sizeof(long long))&0x3f)&0x3f];

	char data[0];
} shared_block_t;

typedef struct {
	long long block_count;
	long long block_size;

	volatile long long read_start;
	volatile long long read_end;

	volatile long long write_start;
	volatile long long write_end;

	sem_t sem_signal; char __padding0[(0x8-sizeof(sem_t)&0x7)&0x7];
	sem_t sem_avail;  char __padding1[(0x8-sizeof(sem_t)&0x7)&0x7];

	char __padding2[(0x40-(3*2*sizeof(long long)+2*sizeof(sem_t)+2*((0x8-sizeof(sem_t)&0x7)&0x7))&0x3f)&0x3f];

	shared_block_t blocks[0];
} shared_mem_t;