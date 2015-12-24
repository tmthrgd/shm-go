#include <semaphore.h> // For sem_*

typedef struct {
	unsigned long long next;
	unsigned long long prev;

	unsigned long long done_read;
	unsigned long long done_write;

	unsigned long long size;

	char flags[(0x40-((2*2+1)*sizeof(long long))&0x3f)&0x3f];

	char data[];
} shared_block_t;

typedef struct {
	unsigned long long block_count;
	unsigned long long block_size;

	unsigned long long read_start;
	unsigned long long read_end;

	unsigned long long write_start;
	unsigned long long write_end;

	sem_t sem_signal; char __padding0[(0x8-sizeof(sem_t)&0x7)&0x7];
	sem_t sem_avail;  char __padding1[(0x8-sizeof(sem_t)&0x7)&0x7];

	char __padding2[(0x40-(3*2*sizeof(long long)+2*sizeof(sem_t)+2*((0x8-sizeof(sem_t)&0x7)&0x7))&0x3f)&0x3f];

	shared_block_t blocks[];
} shared_mem_t;