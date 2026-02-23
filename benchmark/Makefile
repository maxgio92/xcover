CC     ?= gcc
CFLAGS ?= -O0 -g

# Number of distinct functions generated for the miss scenario.
N ?= 1000

.PHONY: all hit idle miss clean

all: hit idle miss

## hit: compile the hit scenario target binary.
hit: target/hit/hit

target/hit/hit: target/hit/main.c
	$(CC) $(CFLAGS) -o $@ $<

## idle: compile the idle scenario target binary.
idle: target/idle/idle

target/idle/idle: target/idle/main.c
	$(CC) $(CFLAGS) -o $@ $<

## miss: generate and compile the miss scenario target binary.
miss: target/miss/miss

target/miss/miss.c: target/miss/gen/main.go
	go run $< -n $(N) > $@

target/miss/miss: target/miss/miss.c
	$(CC) $(CFLAGS) -o $@ $<

## clean: remove all generated and compiled files.
clean:
	rm -f target/hit/hit \
	      target/idle/idle \
	      target/miss/miss \
	      target/miss/miss.c
