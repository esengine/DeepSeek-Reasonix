ifeq ($(OS),Windows_NT)
	WATCOM := C:/WATCOM
	WCC := $(WATCOM)/binnt64/wcc386
	WLINK := $(WATCOM)/binnt64/wlink
	RM := del
	PATHFIX = $(subst /,\,$1)
else
	WATCOM := /opt/watcom
	WCC := $(WATCOM)/binl64/wcc386
	WLINK := $(WATCOM)/binl64/wlink
	RM := rm
	PATHFIX = $1
endif

CFLAGS := \
	'-i$(call PATHFIX,$(WATCOM)/h)' \
	'-i$(call PATHFIX,$(WATCOM)/h/nt)' \
	'-i$(call PATHFIX,$(WATCOM)/h/nt/ddk)' \
	-zl -s -bd -os -d0 -fr= -zq

LDFLAGS := \
	LIBPATH '$(call PATHFIX,$(WATCOM)/lib386)' \
	LIBPATH '$(call PATHFIX,$(WATCOM)/lib386/nt)'

all: tiny.dll

.PHONY: clean

%.o: %.c
	$(WCC) $(CFLAGS) "$<" "-fo=$@"

%.dll: %.o
	$(WLINK) $(LDFLAGS) NAME "$@" @export.def FILE {$<}

clean:
	$(RM) tiny.dll tiny.o
