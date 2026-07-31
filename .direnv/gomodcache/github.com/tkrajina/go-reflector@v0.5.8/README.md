# Golang reflector

First of all, don't use reflection if you don't have to.

But if you really have to... This library offers a simplified Golang reflection abstraction.

## Getting and setting fields

Let's suppose we have structs like:

    type Address struct {
        Street string `tag:"be" tag2:"1,2,3"`
        Number int    `tag:"bi"`
    }

    type Person struct {
        Name string `tag:"bu"`
        Address
    }

    func (p Person) Hi(name string) string {
        return fmt.Sprintf("Hi %s my name is %s", name, p.Name)
    }

Initialize the **reflector**'s object wrapper:

    import "github.com/tkrajina/go-reflector/reflector"

	p := Person{}
	obj := reflector.New(p)

Check if a field is valid:

    obj.Field("Name").IsValid()

Get field value:

    val, err := obj.Field("Name").Get()

Set field value:

	p := Person{}
	obj := reflector.New(&p)
    err := obj.Field("Name").Set("Something")

Don't forget to use a pointer in `New()`, otherwise setters won't work. Field "settability" can be checked by using `field.IsSettable()`.

## Tags

Get a tag:

    jsonTag := obj.Field("Name").Tag("json")

Get tag values array (exploded with "," as a delimiter):

    jsonTag := obj.Field("Name").TagExpanded("json")

Or get a map with all field tags:

    fieldTagsMap := obj.Field("Name").Tags()

## Listing fields

There are three ways to list fields:

 * List all fields: This will include anonymous structs **and** fields declared in  anonymous structs (`Name`, `Address`, `Street`, `Number`).
 * List flattened fields: Includes fields declared in anonymous structs **without**  anonymous structs (`Name`, `Street`, `Number`).
 * List nonflattened fields: Includes anonymous structs **without** their fields (`Name`, `Address`). This is the way fields are actually declared in the code.

Depending on which listing you want, you can use:

    fields := obj.FieldsAll()
    fields := obj.FieldsFlattened()
    fields := obj.Fields()

You can only get the list of anonymous fields with `obj.FieldsAnonymous()`.

Be aware that because of anonymous structs, some field names can be returned twice!
In most cases this is not a desired situation, but you can use **reflector** to detect such situations in your code:

    doubleDeclaredFields := obj.FindDoubleFields()
    if len(doubleDeclaredFields) > 0 {
        fmt.Println("Detected multiple fields with same name:", doubleDeclaredFields)
    }

The field listing will contain both exported and unexported fields. Unexported fields are not gettable/settable, but their tags are readable.

## Calling methods

	obj := reflector.New(&Person{})
    resp, err := obj.Method("Hi").Call("John", "Smith")

The `err` is not `nil` only if something was wrong with the method (for example invalid method name, or wrong argument number/types), not with the actual method call.
If the call finished, `err` will be `nil`.
If the method call returned an error, you can check it with:

    if resp.IsError() {
        fmt.Println("Got an error:", resp.Error.Error())
    } else {
        fmt.Println("Method call response:", resp.Result)
    }

## Listing methods

    for _, method := range obj.Methods() {
        fmt.Println("Method", method.Name(), "with input types", method.InTypes(), "and output types", method.OutTypes())
    }

## Getting length, getting and setting slice/array/string/map elements

Map:

    m := map[string]interface{}{"aaa", 17}
    o := reflector.New(m)
    fmt.Println("Length", o.Len())
    val, found := o.GetByKey("aaa")
    o.SetByKey("bbb", "new value")
    fmt.Println("keys:", o.Keys())

Slice, string:

    l := []int{1, 2, 3}
    o := reflector.New(o)
    fmt.Println("Length", o.Len())
    val, found := o.GetByIndex(0)
    o.SetByIndex(0, 19)

## Performance

When reflecting the same type multiple times, **reflector** will cache as much reflection metadata as possible **only once** and use that in future.

If you make any changes to the library, run `make test-performance` to check performance improvement/deterioration before/after your change.

    $ make test-performance
    N=1000000 go test -v ./... -run=TestPerformance
    === RUN   TestPerformance
    WITH REFLECTION
        n= 1000000
        started: 2016-05-25 08:35:15.5258
        ended: 2016-05-25 08:35:19.5258
        duration: 4.269112s
    --- PASS: TestPerformance (4.27s)
    === RUN   TestPerformancePlain
    WITHOUT REFLECTION
        n= 1000000
        started: 2016-05-25 08:35:19.5258
        ended: 2016-05-25 08:35:19.5258
        duration: 0.005237s
    --- PASS: TestPerformancePlain (0.01s)
    PASS
    ok      github.com/tkrajina/go-reflector/reflector      4.285s

Keep those numbers in mind before deciding to use reflection :)

License
-------

**Reflector** is licensed under the [Apache License, Version 2.0](http://www.apache.org/licenses/LICENSE-2.0)
