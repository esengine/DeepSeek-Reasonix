package main

/*
#include "callback.h"

void call_later_go_cb(void*);
*/
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/mattn/go-pointer"
)

type Foo struct {
	v int
}

func main() {
	f := &Foo{123}
	C.call_later(3, C.callback(C.call_later_go_cb), pointer.Save(f))
}

//export call_later_go_cb
func call_later_go_cb(data unsafe.Pointer) {
	f := pointer.Restore(data).(*Foo)
	fmt.Println(f.v)
}
