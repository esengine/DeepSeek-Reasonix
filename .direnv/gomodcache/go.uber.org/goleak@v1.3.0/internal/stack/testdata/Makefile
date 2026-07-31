.DEFAULT_GOAL := all

GO_VERSION = $(shell go version | cut -d' ' -f3)

# Append to this list to add new stacks.
STACKS =

# In Go 1.21, the output format was changed slightly.
#
# Generate a 1.20 version of the output
# only if we're running on Go 1.20.
ifneq (,$(findstring go1.20,$(GO_VERSION)))
STACKS += http.go1.20.txt
http.go1.20.txt: http.go
	go run $< > $@
else
STACKS += http.txt
http.txt: http.go
	go run $< > $@
endif

STACKS += http.tracebackancestors.txt
http.tracebackancestors.txt: http.go
	GODEBUG=tracebackancestors=10 go run $< > $@

.PHONY: all
all: $(STACKS)
