#include <semaphore.h> // For sem_*

typedef struct {
	long long next;
	long long prev;

	long long done_read;
	long long done_write;

	long long size;

	char flags[(0x40-((2*2+1)*sizeof(long long))&0x3f)&0x3f];

	char data[];
} shared_block_t;

typedef struct {
	long long block_count;
	long long block_size;

	long long read_start;
	long long read_end;

	long long write_start;
	long long write_end;

	sem_t sem_signal; char __padding0[(0x8-sizeof(sem_t)&0x7)&0x7];
	sem_t sem_avail;  char __padding1[(0x8-sizeof(sem_t)&0x7)&0x7];

	char __padding2[(0x40-(3*2*sizeof(long long)+2*sizeof(sem_t)+2*((0x8-sizeof(sem_t)&0x7)&0x7))&0x3f)&0x3f];

	shared_block_t blocks[];
} shared_mem_t;