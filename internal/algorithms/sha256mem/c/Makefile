CC      = gcc
CFLAGS  = -O2 -Wall -Wextra
LDFLAGS = -lcrypto

all: test_sha256mem

test_sha256mem: test_sha256mem.c sha256mem.c sha256mem.h
	$(CC) $(CFLAGS) -o $@ test_sha256mem.c sha256mem.c $(LDFLAGS)

vectors: gen_vectors.go
	cd ../../../.. && go run ./internal/algorithms/sha256mem/c/gen_vectors.go
	mv ../../../../test_vectors.txt ./ 2>/dev/null || true

test: vectors test_sha256mem
	./test_sha256mem test_vectors.txt

clean:
	rm -f test_sha256mem test_vectors.txt

.PHONY: all vectors test clean
