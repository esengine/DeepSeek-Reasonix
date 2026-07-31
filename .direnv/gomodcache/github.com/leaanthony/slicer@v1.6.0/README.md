
<div style="text-align:center; width:400px">
  <img src="logo.png"/>
  Utility class for handling slices.
</div>


[![Go Report Card](https://goreportcard.com/badge/github.com/leaanthony/slicer)](https://goreportcard.com/report/github.com/leaanthony/slicer)  [![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg)](http://godoc.org/github.com/leaanthony/slicer) [![CodeFactor](https://www.codefactor.io/repository/github/leaanthony/slicer/badge)](https://www.codefactor.io/repository/github/leaanthony/slicer) [![codecov](https://codecov.io/gh/leaanthony/slicer/branch/master/graph/badge.svg)](https://codecov.io/gh/leaanthony/slicer) [![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)  





## Install

`go get -u github.com/leaanthony/slicer`

## Quick Start

```
  import "github.com/leaanthony/slicer"

  func test() {
    s := slicer.String()
    s.Add("one")
    s.Add("two")
    s.AddSlice([]string{"three","four"})
    fmt.Printf("My slice = %+v\n", s.AsSlice())
    
    t := slicer.String()
    t.Add("zero")
    t.AddSlicer(s)
    fmt.Printf("My slice = %+v\n", t.AsSlice())
  }
```

## Available slicers

- Int
- Int8
- Int16
- Int32
- Int64  
- UInt
- UInt8
- UInt16
- UInt32
- UInt64
- Float32
- Float64
- String
- Bool
- Interface
  
## API

### Construction

Create new Slicers by calling one of the following functions:
  - Int()
  - Int8()
  - Int16()
  - Int32()
  - Int64()
  - Float32()
  - Float64()
  - String()
  - Bool()
  - Interface()

```
  s := slicer.String()
```

If you wish to convert an existing slice to a Slicer, you may pass it in during creation:

```
  values := []string{"one", "two", "three"}
  s := slicer.String(values)
```

### Add 

Adds a value to the slice.

```
  values := []string{"one", "two", "three"}
  s := slicer.String(values)
  s.Add("four")
```

### AddUnique

Adds a value to the slice if it doesn't already contain it.

```
  values := []string{"one", "two", "three", "one", "two", "three"}
  s := slicer.String(values)
  result := s.Join(",")
  // result is "one,two,three"
```
### AddSlice

Adds an existing slice of values to a slicer

```
  s := slicer.String([]string{"one"})
  s.AddSlice([]string{"two"})
```

### AsSlice

Returns a regular slice from the slicer.

```
  s := slicer.String([]string{"one"})
  for _, value := range s.AsSlice() {
    ...
  }
```

### AddSlicer

Adds an existing slicer of values to another slicer

```
  a := slicer.String([]string{"one"})
  b := slicer.String([]string{"two"})
  a.AddSlicer(b)
```

### Filter

Filter the values of a slicer based on the result of calling the given function with each value of the slice. If it returns true, the value is added to the result.

```
  a := slicer.Int([]int{1,5,7,9,6,3,1,9,1})
  result := a.Filter(func(v int) bool {
    return v > 5
  })
  // result is []int{7,9,9}
  
```

### Each 

Each iterates over all the values of a slicer, passing them in as paramter to a function

```
  a := slicer.Int([]int{1,5,7,9,6,3,1,9,1})
  result := 0
  a.Each(func(v int) {
    result += v
  })
  // result is 42
```

### Contains

Contains returns true if the slicer contains the given value

```
  a := slicer.Int([]int{1,5,7,9,6,3,1,9,1})
  result := a.Contains(9)
  // result is True
```

### Join

Returns a string with the slicer elements separated by the given separator

```
  a := slicer.String([]string{"one", "two", "three"})
  result := a.Join(",")
  // result is "one,two,three"
```
### Length

Returns the length of the slice

```
  a := slicer.String([]string{"one", "two", "three"})
  result := a.Length()
  // result is 3
```

### Clear

Clears all elements from the current slice

```
  a := slicer.String([]string{"one", "two", "three"})
  a.Clear()
  // a.Length() == 0
```

### Sort

Sorts the elements of a slice
Not supported by: InterfaceSlicer, BoolSlicer

```
  a := slicer.Int([]int{5,3,4,1,2})
  a.Sort()
  // a is []int{1,2,3,4,5}
```

### Deduplicate

Deduplicate removes all duplicates within a slice.

```
  a := slicer.Int([]int{5,3,5,1,3})
  a.Deduplicate()
  // a is []int{5,3,1}
```
