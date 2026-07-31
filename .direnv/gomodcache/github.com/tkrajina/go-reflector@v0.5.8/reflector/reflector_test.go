package reflector

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tkrajina/go-reflector/reflector/tmp"
)

type Address struct {
	Street string `tag:"be" tag2:"1,2,3"`
	Number int    `tag:"bi"`
}

type Person struct {
	Name string `tag:"bu"`
	Address
}

func (p Person) Add(a, b, c int) int    { return a + b + c }
func (p *Person) Subtract(a, b int) int { return a - b }
func (p Person) ReturnsError(err bool) (string, *int, error) {
	i := 2
	if err {
		return "", nil, errors.New("error here")
	}
	return "jen", &i, nil
}

type CustomType int

func (ct CustomType) Method1() string { return "yep" }
func (ct *CustomType) Method2() int   { return 7 }

func (p Person) Hi(name string) string {
	return fmt.Sprintf("Hi %s my name is %s", name, p.Name)
}

type Company struct {
	Address
	Number int `tag:"bi"`
}

// TestDoubleFields checks that there are no "double" fields (or fields shadowing)
func TestDoubleFields(t *testing.T) {
	t.Parallel()
	structs := []interface{}{
		Obj{},
		ObjField{},
		ObjMethod{},
		ObjMetadata{},
		ObjFieldMetadata{},
		ObjMethodMetadata{},
	}
	for _, s := range structs {
		double := New(s).FindDoubleFields()
		assert.Equal(t, 0, len(double))
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "reflector.Person", New(Person{}).String())
	assert.Equal(t, "*reflector.Person", New(&Person{}).String())
	assert.Equal(t, "nil", New(nil).String())
	assert.Equal(t, "int", New(1).String())
	var i int
	assert.Equal(t, "*int", New(&i).String())

	// Check that when we twice get a field, the metadata field is cached only once:
	assert.Equal(t, New(Person{}).Field("bu").ObjFieldMetadata, New(Person{}).Field("bu").ObjFieldMetadata)
	assert.Equal(t, New(&Person{}).Field("bu").ObjFieldMetadata, New(&Person{}).Field("bu").ObjFieldMetadata)
	assert.Equal(t, New(Person{}).Field("Address").ObjFieldMetadata, New(Person{}).Field("Address").ObjFieldMetadata)
	assert.Equal(t, New(&Person{}).Field("bu").ObjFieldMetadata, New(&Person{}).Field("bu").ObjFieldMetadata)
}

func TestNilStringPtr(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "*string", New((*string)(nil)).String())
	assert.Equal(t, 0, len(New((*string)(nil)).Fields()))
	assert.Equal(t, 0, len(New((*string)(nil)).Methods()))

	v, err := New((*string)(nil)).Field("Bu").Get()
	assert.Nil(t, v)
	assert.NotNil(t, err)
}

func TestNilStructPtr(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "*reflector.Person", New((*Person)(nil)).String())
	assert.Equal(t, 2, len(New((*Person)(nil)).Fields()))
	assert.Equal(t, 4, len(New((*Person)(nil)).Methods()))

	err := New((*Person)(nil)).Field("Number").Set(17)
	assert.NotNil(t, err)

	v, err := New((*Person)(nil)).Field("Bu").Get()
	assert.Nil(t, v)
	assert.NotNil(t, err)
}

func TestListFieldsFlattened(t *testing.T) {
	t.Parallel()
	p := Person{}
	obj := New(p)

	assert.True(t, obj.IsValid())
	assert.False(t, obj.IsPtr())
	assert.True(t, obj.IsStructOrPtrToStruct())

	fields := obj.FieldsFlattened()
	assert.Equal(t, 3, len(fields))
	assert.Equal(t, fields[0].Name(), "Name")
	assert.Equal(t, fields[1].Name(), "Street")
	assert.Equal(t, fields[2].Name(), "Number")

	assert.True(t, obj.Field("Name").IsValid())

	kind := obj.Field("Name").Kind()
	assert.Equal(t, reflect.String, kind)

	kind = obj.Field("BuName").Kind()
	assert.Equal(t, reflect.Invalid, kind)

	ty := obj.Field("Number").Type()
	assert.Equal(t, reflect.TypeOf(1), ty)

	ty = obj.Field("Istra").Type()
	assert.Nil(t, ty)
}

func TestListFields(t *testing.T) {
	t.Parallel()
	p := Person{}
	obj := New(p)

	fields := obj.Fields()
	assert.Equal(t, len(fields), 2)
	assert.Equal(t, fields[0].Name(), "Name")
	assert.Equal(t, fields[1].Name(), "Address")
}

func TestListFieldsAll(t *testing.T) {
	t.Parallel()
	p := Person{}
	obj := New(p)

	fields := obj.FieldsAll()
	assert.Equal(t, len(fields), 4)
	assert.Equal(t, fields[0].Name(), "Name")
	assert.Equal(t, fields[1].Name(), "Address")
	assert.Equal(t, fields[2].Name(), "Street")
	assert.Equal(t, fields[3].Name(), "Number")
}

func TestListFieldsAnonymous(t *testing.T) {
	t.Parallel()
	p := Person{}
	obj := New(p)

	fields := obj.FieldsAnonymous()
	assert.Equal(t, 1, len(fields))
	assert.Equal(t, fields[0].Name(), "Address")
}

func TestListFieldsAllWithDoubleFields(t *testing.T) {
	t.Parallel()
	obj := New(Company{})

	fields := obj.FieldsAll()
	assert.Equal(t, len(fields), 4)
	assert.Equal(t, fields[0].Name(), "Address")
	assert.Equal(t, fields[1].Name(), "Street")
	// Number is declared both in Company and in Address, so listed twice here:
	assert.Equal(t, fields[2].Name(), "Number")
	assert.Equal(t, fields[3].Name(), "Number")
}

func TestFindDoubleFields(t *testing.T) {
	t.Parallel()
	obj := New(Company{})

	fields := obj.FindDoubleFields()
	assert.Equal(t, 1, len(fields))
	assert.Equal(t, fields[0], "Number")
}

func TestListFieldsOnPointer(t *testing.T) {
	t.Parallel()
	p := &Person{}
	obj := New(p)

	assert.True(t, obj.IsPtr())
	assert.True(t, obj.IsStructOrPtrToStruct())

	fields := obj.Fields()
	assert.Equal(t, len(fields), 2)
	assert.Equal(t, fields[0].Name(), "Name")
	assert.Equal(t, fields[1].Name(), "Address")

	kind := obj.Field("Name").Kind()
	assert.Equal(t, reflect.String, kind)

	kind = obj.Field("BuName").Kind()
	assert.Equal(t, reflect.Invalid, kind)

	ty := obj.Field("Number").Type()
	assert.Equal(t, reflect.TypeOf(1), ty)

	ty = obj.Field("Istra").Type()
	assert.Nil(t, ty)
}

func TestListFieldsFlattenedOnPointer(t *testing.T) {
	t.Parallel()
	p := &Person{}
	obj := New(p)

	fields := obj.FieldsFlattened()
	assert.Equal(t, len(fields), 3)
	assert.Equal(t, fields[0].Name(), "Name")
	assert.Equal(t, fields[1].Name(), "Street")
	assert.Equal(t, fields[2].Name(), "Number")
}

func TestNoFieldsNoCustomType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, len(New(CustomType(1)).Fields()), 0)
	ct := CustomType(2)
	assert.Equal(t, len(New(&ct).Fields()), 0)
}

func TestIsStructForCustomTypes(t *testing.T) {
	t.Parallel()
	ct := CustomType(2)
	assert.False(t, New(CustomType(1)).IsPtr())
	assert.True(t, New(&ct).IsPtr())
	assert.False(t, New(CustomType(1)).IsStructOrPtrToStruct())
	assert.False(t, New(&ct).IsStructOrPtrToStruct())
}

func TestFieldValidity(t *testing.T) {
	t.Parallel()
	assert.False(t, New(CustomType(1)).Field("jkljkl").IsValid())
	assert.False(t, New(Person{}).Field("street").IsValid())
	assert.True(t, New(Person{}).Field("Street").IsValid())
	assert.True(t, New(Person{}).Field("Number").IsValid())
	assert.True(t, New(Person{}).Field("Name").IsValid())
}

func TestSetFieldNonPointer(t *testing.T) {
	t.Parallel()
	p := Person{}
	obj := New(p)
	assert.False(t, obj.IsPtr())

	field := obj.Field("Street")
	assert.False(t, field.IsSettable())

	err := field.Set("ulica")
	assert.Error(t, err)

	value, err := field.Get()
	assert.Nil(t, err)
	assert.Equal(t, "", value)

	assert.Equal(t, "", p.Street)

	street, err := obj.Field("Street").Get()
	assert.Nil(t, err)

	// This actually don't work because p is a struct and reflector is working on it's own copy:
	assert.Equal(t, "", street)

}

func TestSetField(t *testing.T) {
	t.Parallel()
	p := Person{}
	obj := New(&p)
	assert.True(t, obj.IsPtr())

	field := obj.Field("Street")
	assert.True(t, field.IsSettable())

	err := field.Set("ulica")
	assert.Nil(t, err)

	value, err := field.Get()
	assert.Nil(t, err)
	assert.Equal(t, "ulica", value)

	assert.Equal(t, "ulica", p.Street)
}

func TestCustomTypeMethods(t *testing.T) {
	t.Parallel()
	assert.Equal(t, len(New(CustomType(1)).Methods()), 1)
	ct := CustomType(1)
	assert.Equal(t, len(New(&ct).Methods()), 2)
}

func TestMethods(t *testing.T) {
	t.Parallel()
	assert.Equal(t, len(New(Person{}).Methods()), 3)
	assert.Equal(t, len(New(&Person{}).Methods()), 4)

	// Check that when we twice get a field, the metadata field is cached only once:
	assert.Equal(t, New(Person{}).Method("Add").ObjMethodMetadata, New(Person{}).Method("Add").ObjMethodMetadata)
	assert.Equal(t, New(&Person{}).Method("Add").ObjMethodMetadata, New(&Person{}).Method("Add").ObjMethodMetadata)
}

func TestCallMethod(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})
	method := obj.Method("Add")
	res, err := method.Call(2, 3, 6)
	assert.Nil(t, err)
	assert.False(t, res.IsError())
	assert.Equal(t, len(res.Result), 1)
	assert.Equal(t, res.Result[0], 11)

	assert.True(t, method.IsValid())
	assert.Equal(t, len(method.InTypes()), 3)
	assert.Equal(t, len(method.OutTypes()), 1)

	sub, err := obj.Method("Substract").Call(5, 6)
	assert.Nil(t, err)
	assert.Equal(t, sub.Result, []interface{}{-1})
}

func TestCallInvalidMethod(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})
	method := obj.Method("AddAdddd")
	res, err := method.Call([]interface{}{2, 3, 6})
	assert.NotNil(t, err)
	assert.Nil(t, res)

	assert.Equal(t, len(method.InTypes()), 0)
	assert.Equal(t, len(method.OutTypes()), 0)
}

func TestMethodsValidityOnPtr(t *testing.T) {
	t.Parallel()
	ct := CustomType(1)
	obj := New(&ct)

	assert.True(t, obj.IsPtr())

	assert.True(t, obj.Method("Method1").IsValid())
	assert.True(t, obj.Method("Method2").IsValid())

	{
		res, err := obj.Method("Method1").Call()
		assert.Nil(t, err)
		assert.Equal(t, res.Result, []interface{}{"yep"})
	}
	{
		res, err := obj.Method("Method2").Call()
		assert.Nil(t, err)
		assert.Equal(t, res.Result, []interface{}{7})
	}
}

func TestMethodsValidityOnNonPtr(t *testing.T) {
	t.Parallel()
	obj := New(CustomType(1))

	assert.False(t, obj.IsPtr())

	assert.True(t, obj.Method("Method1").IsValid())
	// False because it's not a pointer
	assert.False(t, obj.Method("Method2").IsValid())

	{
		res, err := obj.Method("Method1").Call()
		assert.Nil(t, err)
		assert.Equal(t, res.Result, []interface{}{"yep"})
	}
	{
		_, err := obj.Method("Method2").Call()
		assert.NotNil(t, err)
	}
}

func testCallMethod(t *testing.T, callValue bool, lenResult int) bool {
	obj := New(&Person{})
	res, err := obj.Method("ReturnsError").Call(callValue)
	assert.Nil(t, err)
	assert.Equal(t, len(res.Result), lenResult)
	return res.IsError()
}

func TestCallMethodWithoutErrResult(t *testing.T) {
	t.Parallel()
	isErr := testCallMethod(t, true, 3)
	assert.True(t, isErr)
}

func TestCallMethodWithErrResult(t *testing.T) {
	t.Parallel()
	isErr := testCallMethod(t, false, 3)
	assert.False(t, isErr)
}

func TestTag(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})
	tag, err := obj.Field("Street").Tag("invalid")
	assert.Nil(t, err)
	assert.Equal(t, len(tag), 0)
}

func TestInvalidTag(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})
	tag, err := obj.Field("HahaStreet").Tag("invalid")
	assert.NotNil(t, err)
	assert.Equal(t, "invalid field HahaStreet", err.Error())
	assert.Equal(t, len(tag), 0)
}

func TestValidTag(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})
	tag, err := obj.Field("Street").Tag("tag")
	assert.Nil(t, err)
	assert.Equal(t, tag, "be")
}

func TestValidTags(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})

	tags, err := obj.Field("Street").TagExpanded("tag")
	assert.Nil(t, err)
	assert.Equal(t, tags, []string{"be"})

	tags2, err := obj.Field("Street").TagExpanded("tag2")
	assert.Nil(t, err)
	assert.Equal(t, tags2, []string{"1", "2", "3"})
}

func TestAllTags(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})

	tags, err := obj.Field("Street").Tags()
	assert.Nil(t, err)
	assert.Equal(t, len(tags), 2)
	assert.Equal(t, tags["tag"], "be")
	assert.Equal(t, tags["tag2"], "1,2,3")

	/*
			type Address struct {
			Street string `tag:"be" tag2:"1,2,3"`
			Number int    `tag:"bi"`
		}
	*/
	fld := New(Address{}).Field("Street")
	tagsStr, err := fld.TagsString()
	assert.Nil(t, err)
	assert.Equal(t, `tag:"be" tag2:"1,2,3"`, tagsStr)
}

func TestNewFromType(t *testing.T) {
	t.Parallel()
	obj1 := NewFromType(reflect.TypeOf(Person{}))
	obj2 := New(&Person{})

	assert.Equal(t, obj1.objType.String(), obj2.objType.String())
	assert.Equal(t, obj1.objKind.String(), obj2.objKind.String())
	assert.Equal(t, obj1.underlyingType.String(), obj2.underlyingType.String())
}

func TestAnonymousFields(t *testing.T) {
	t.Parallel()
	obj := New(&Person{})

	assert.True(t, obj.Field("Address").IsAnonymous())
	assert.False(t, obj.Field("Name").IsAnonymous())
}

func TestNil(t *testing.T) {
	t.Parallel()
	obj := New(nil)
	assert.Equal(t, 0, len(obj.Fields()))
	assert.Equal(t, 0, len(obj.Methods()))

	res, err := obj.Field("Aaa").Get()
	assert.Nil(t, res)
	assert.NotNil(t, err)

	err = obj.Field("Aaa").Set(1)
	assert.NotNil(t, err)
}

func TestNilType(t *testing.T) {
	t.Parallel()
	obj := NewFromType(nil)
	assert.Equal(t, 0, len(obj.Fields()))
	assert.Equal(t, 0, len(obj.Methods()))

	res, err := obj.Field("Aaa").Get()
	assert.Nil(t, res)
	assert.NotNil(t, err)

	err = obj.Field("Aaa").Set(1)
	assert.NotNil(t, err)

	_, err = obj.Method("aaa").Call("bu")
	assert.NotNil(t, err)
}

func TestStringObj(t *testing.T) {
	t.Parallel()
	obj := New("")
	assert.Equal(t, 0, len(obj.Fields()))
	assert.Equal(t, 0, len(obj.Methods()))

	res, err := obj.Field("Aaa").Get()
	assert.Nil(t, res)
	assert.NotNil(t, err)

	err = obj.Field("Aaa").Set(1)
	assert.NotNil(t, err)

	_, err = obj.Method("aaa").Call("bu")
	assert.NotNil(t, err)
}

type TestWithInnerStruct struct {
	Aaa string
	Bbb struct {
		Ccc int
		Ddd float64
	}
}

func TestInnerStruct(t *testing.T) {
	t.Parallel()
	obj := New(TestWithInnerStruct{})
	fields := obj.Fields()
	assert.Equal(t, 2, len(fields))

	assert.Equal(t, "Aaa", fields[0].Name())
	assert.Equal(t, "string", fields[0].Type().String())
	assert.Equal(t, "string", fields[0].Kind().String())

	assert.Equal(t, "Bbb", fields[1].Name())
	assert.Equal(t, "struct { Ccc int; Ddd float64 }", fields[1].Type().String())
	assert.Equal(t, "struct", fields[1].Kind().String())

	// This is not an anonymous struct, so fields are always the same:
	assert.Equal(t, 2, len(obj.FieldsAll()))
	assert.Equal(t, 2, len(obj.FieldsFlattened()))
}

func TestExportedUnexported(t *testing.T) {
	t.Parallel()
	obj := New(&tmp.TestStruct{})
	assert.Equal(t, "_", obj.Fields()[0].Name())
	assert.False(t, obj.Fields()[0].IsExported())

	assert.Equal(t, "Exported", obj.Fields()[1].Name())
	assert.True(t, obj.Fields()[1].IsExported())

	assert.Equal(t, "unexported", obj.Fields()[2].Name())
	assert.False(t, obj.Fields()[2].IsExported())

	err := obj.Field("Exported").Set("aaa")
	assert.Nil(t, err)

	err = obj.Field("unexported").Set(1777)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "not settable")

	value, err := obj.Field("unexported").Get()
	assert.NotNil(t, err)
	assert.Nil(t, value)
	assert.Contains(t, err.Error(), "cannot read unexported field")

	// But tags on unexported fields are still readable:
	{
		tags, err := obj.Field("_").Tags()
		assert.Nil(t, err)
		assert.Equal(t, 1, len(tags))
		assert.Equal(t, "ba", tags["bu"])
	}
	{
		tags, err := obj.Field("unexported").Tags()
		assert.Nil(t, err)
		assert.Equal(t, 2, len(tags))
	}
}

func TestStringLen(t *testing.T) {
	s := "jkljk"
	assert.Equal(t, len(s), New(s).Len())
	assert.Equal(t, len(s), New(&s).Len())
}

func TestLenOfInvalidTypes(t *testing.T) {
	assert.Equal(t, 0, New(&tmp.TestStruct{}).Len())
	assert.Equal(t, 0, New(tmp.TestStruct{}).Len())

	{
		val, found := New(tmp.TestStruct{}).GetByIndex(20)
		assert.False(t, found)
		assert.Nil(t, val)
	}
	{
		val, found := New(tmp.TestStruct{}).GetByKey("nothing")
		assert.False(t, found)
		assert.Equal(t, nil, val)
	}
	{
		keys, err := New(tmp.TestStruct{}).Keys()
		assert.Nil(t, keys)
		assert.NotNil(t, err)
	}
}

func TestSlice(t *testing.T) {
	{
		a := []int{1, 2, 3, 4, 8, 7, 6, 5}
		assert.Equal(t, len(a), New(a).Len())
		assert.Equal(t, len(a), New(&a).Len())
		{
			o := New(&a)
			assert.Nil(t, o.SetByIndex(1, 1000))
			assert.Equal(t, []int{1, 1000, 3, 4, 8, 7, 6, 5}, a)
		}
		{
			o := New(&a)
			assert.NotNil(t, o.SetByIndex(-1, 1000))
			assert.NotNil(t, o.SetByIndex(700, 1000))
		}
		{
			o := New(a)
			assert.Nil(t, o.SetByIndex(1, 1000))
			assert.Equal(t, []int{1, 1000, 3, 4, 8, 7, 6, 5}, a)
		}
		{
			val, found := New(a).GetByIndex(2)
			assert.True(t, found)
			assert.Equal(t, val, 3)
		}
		{
			val, found := New(a).GetByIndex(20)
			assert.False(t, found)
			assert.Nil(t, val)
		}
		{
			val, found := New(a).GetByIndex(-20)
			assert.False(t, found)
			assert.Nil(t, val)
		}
	}
}

func TestMapLenGetSet(t *testing.T) {
	m := map[string]interface{}{"jkljk": 8, "11": 13, "12": nil}
	assert.Equal(t, len(m), New(m).Len())
	assert.Equal(t, len(m), New(&m).Len())
	{
		keys, err := New(&m).Keys()
		assert.Nil(t, err)
		assert.Equal(t, 3, len(keys))
		keysStrings := make([]string, len(keys))
		for n := range keys {
			keysStrings[n] = keys[n].(string)
		}
		sort.Strings(keysStrings)
		assert.Equal(t, []string{"11", "12", "jkljk"}, keysStrings)
	}
	{
		val, found := New(&m).GetByKey("11")
		assert.True(t, found)
		assert.Equal(t, 13, val)
	}
	{
		val, found := New(m).GetByKey("11")
		assert.True(t, found)
		assert.Equal(t, 13, val)
	}
	{
		val, found := New(m).GetByKey("12")
		assert.True(t, found)
		assert.Equal(t, nil, val)
	}
	{
		val, found := New(m).GetByKey("nothing")
		assert.False(t, found)
		assert.Equal(t, nil, val)
	}
	{
		val, found := New(m).GetByKey(17)
		assert.False(t, found)
		assert.Equal(t, nil, val)
	}

}

func TestMapSet(t *testing.T) {
	m := map[string]interface{}{"jkljk": 8, "11": 13, "12": nil}
	o := New(&m)
	assert.Nil(t, o.SetByKey("17", 71))
	val, found := m["17"]
	assert.True(t, found)
	assert.Equal(t, 71, val)
}

func TestSetStringByIndex(t *testing.T) {
	s := "jkljkl"
	o := New(&s)
	assert.NotNil(t, o.SetByIndex(0, 'a'))
}

func TestWalk(t *testing.T) {
	t.Parallel()

	s := struct {
		String      string
		PtrToString *string
		Map         map[string]int
		PtrToMap    *map[string]int
		Slice       []string
		PtrToSlice  *[]string
		Array       [2]int
		PtrToArray  *[2]int
		Struct      struct{ Name string }
	}{}
	_ = s
}

func TestDereferencedIface(t *testing.T) {
	t.Parallel()

	ptrPerson := &Person{Name: "aaa"}
	for _, i := range []interface{}{
		Person{Name: "aaa"},
		&Person{Name: "aaa"},
		&ptrPerson,
	} {
		fmt.Printf("type %T:\n", i)
		o := New(i)
		p := o.Dereferenced()
		_, is := p.(Person)
		assert.True(t, is, "found %T", p)
	}
}
