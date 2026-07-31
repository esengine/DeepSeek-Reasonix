# go-pointer

Utility for cgo

## Usage

https://github.com/golang/proposal/blob/master/design/12416-cgo-pointers.md

In go 1.6, cgo argument can't be passed Go pointer.

```
var s string
C.pass_pointer(pointer.Save(&s))
v := *(pointer.Restore(C.get_from_pointer()).(*string))
```

## Installation

```
go get github.com/mattn/go-pointer
```

## License

MIT

## Author

Yasuhiro Matsumoto (a.k.a mattn)
