package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	types := []string{"string", "int64", "int32", "int16", "int8", "int", "uint64", "uint32", "uint16", "uint8", "uint", "float64", "float32", "bool", "interface"}

	// Read template
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	for _, t := range types {
		bytes, err := ioutil.ReadFile(filepath.Join(cwd, "slicer.template"))
		if err != nil {
			panic(err)
		}
		template := string(bytes)
		thisTemplate := template
		Type := t
		TypeTitle := strings.Title(t)

		if Type == "interface" {
			Type = "interface{}"
		}
		SlicerName := TypeTitle + "Slicer"

		// Type specific additions
		thisTemplate = fixup(thisTemplate, t)

		// Replace type
		thisTemplate = strings.ReplaceAll(thisTemplate, "_TYPE_", Type)
		thisTemplate = strings.ReplaceAll(thisTemplate, "_TYPETITLE_", TypeTitle)
		thisTemplate = strings.ReplaceAll(thisTemplate, "_SLICERNAME_", SlicerName)

		// Save file
		targetFile := filepath.Join(cwd, "..", t+".go")
		err = ioutil.WriteFile(targetFile, []byte(thisTemplate), 0644)
		if err != nil {
			panic(err)
		}
	}

}

func fixup(template string, t string) string {

	template = fixupJoin(template, t)
	template = fixupSort(template, t)

	return template
}

func fixupSort(template string, t string) string {
	// Sort String
	sortString := `
// Sort the slice values
func (s *StringSlicer) Sort() {
	sort.Strings(s.slice)
}
`
	sortDefault := `
// Sort the slice values
func (s *_SLICERNAME_) Sort() {
	sort.Slice(s.slice, func(i, j int) bool { return s.slice[i] < s.slice[j] })
}`

	switch t {
	case "string":
		template += sortString
		template = addImport(template, "sort")
	case "interface", "bool": //Ignore
	default:
		template += sortDefault
		template = addImport(template, "sort")
	}
	return template
}

func fixupJoin(template string, t string) string {

	//Join
	joinGeneric := `
// Join returns a string with the slicer elements separated by the given separator
func (s *_SLICERNAME_) Join(separator string) string {
	var builder strings.Builder

	// Shortcut no elements
	if len(s.slice) == 0 {
		return ""
	}

	// Iterate over length - 1
	index := 0
	for index = 0; index < len(s.slice)-1; index++ {
		builder.WriteString(fmt.Sprintf("%v%s", s.slice[index], separator))
	}
	builder.WriteString(fmt.Sprintf("%v", s.slice[index]))
	result := builder.String()
	return result
}`
	joinString := `
// Join returns a string with the slicer elements separated by the given separator
func (s *StringSlicer) Join(separator string) string {
	return strings.Join(s.slice, separator)
}`

	switch t {
	case "string":
		template += joinString
		// template = addImport(template, "sort")
	default:
		template += joinGeneric
		template = addImport(template, "fmt")
	}
	return template
}

func addImport(template string, name string) string {
	newText := fmt.Sprintf("// Imports\nimport \"%s\"", name)
	return strings.ReplaceAll(template, "// Imports", newText)
}
