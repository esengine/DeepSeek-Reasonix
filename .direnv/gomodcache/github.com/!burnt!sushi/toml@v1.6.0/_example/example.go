package main

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type (
	example struct {
		Title      string
		Desc       string
		Integers   []int
		Floats     []float64
		Times      []fmtTime
		Duration   []time.Duration
		Distros    []distro
		Servers    map[string]server
		Characters map[string][]struct {
			Name string
			Rank string
		}
	}

	server struct {
		IP       string
		Hostname string
		Enabled  bool
	}

	distro struct {
		Name     string
		Packages string
	}

	fmtTime struct{ time.Time }
)

func (t fmtTime) String() string {
	f := "2006-01-02 15:04:05.999999999"
	if t.Time.Hour() == 0 {
		f = "2006-01-02"
	}
	if t.Time.Year() == 0 {
		f = "15:04:05.999999999"
	}
	if t.Time.Location() == time.UTC {
		f += " UTC"
	} else {
		f += " -0700"
	}
	return t.Time.Format(`"` + f + `"`)
}

func main() {
	f := "example.toml"
	if _, err := os.Stat(f); err != nil {
		f = "_example/example.toml"
	}

	var config example
	meta, err := toml.DecodeFile(f, &config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	indent := strings.Repeat(" ", 14)

	fmt.Print("Decoded")
	typ, val := reflect.TypeOf(config), reflect.ValueOf(config)
	for i := 0; i < typ.NumField(); i++ {
		indent := indent
		if i == 0 {
			indent = strings.Repeat(" ", 7)
		}
		fmt.Printf("%s%-11s → %v\n", indent, typ.Field(i).Name, val.Field(i).Interface())
	}

	fmt.Print("\nKeys")
	keys := meta.Keys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for i, k := range keys {
		indent := indent
		if i == 0 {
			indent = strings.Repeat(" ", 10)
		}
		fmt.Printf("%s%-10s %s\n", indent, meta.Type(k...), k)
	}

	fmt.Print("\nUndecoded")
	keys = meta.Undecoded()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for i, k := range keys {
		indent := indent
		if i == 0 {
			indent = strings.Repeat(" ", 5)
		}
		fmt.Printf("%s%-10s %s\n", indent, meta.Type(k...), k)
	}
}
