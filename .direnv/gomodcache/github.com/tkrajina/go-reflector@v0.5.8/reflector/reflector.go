package reflector

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

type fieldListingType int

const (
	fieldsAll fieldListingType = iota
	fieldsAnonymous
	fieldsFlattenAnonymous
	fieldsNoFlattenAnonymous
)

var (
	metadataCache      map[reflect.Type]ObjMetadata
	metadataCacheMutex sync.RWMutex
)

func init() {
	metadataCache = map[reflect.Type]ObjMetadata{}
}

func updateCache(ty reflect.Type, o *Obj) {
	metadataCacheMutex.Lock()
	defer metadataCacheMutex.Unlock()

	metadataCache[ty] = o.ObjMetadata
}

// ObjMetadata contains data which is always unique per Type.
type ObjMetadata struct {
	isStruct      bool
	isPtrToStruct bool

	// If ptr to struct, this field will contain the type of that struct
	underlyingType reflect.Type

	objType reflect.Type
	objKind reflect.Kind

	fields map[string]ObjFieldMetadata

	fieldNamesAll                []string
	fieldNamesAnonymous          []string
	fieldNamesFlattenAnonymous   []string
	fieldNamesNoFlattenAnonymous []string

	methods     map[string]ObjMethodMetadata
	methodNames []string
}

func newObjMetadata(ty reflect.Type) *ObjMetadata {
	res := new(ObjMetadata)
	if ty == nil {
		res.objKind = reflect.Invalid
		return res
	}

	res.objType = ty
	res.objKind = res.objType.Kind()

	if ty.Kind() == reflect.Struct {
		res.isStruct = true
	}
	if ty.Kind() == reflect.Ptr && ty.Elem().Kind() == reflect.Struct {
		ty = ty.Elem()
		res.isPtrToStruct = true
	}
	res.underlyingType = ty

	allFields := res.getFields(res.objType, fieldsAll)

	res.fieldNamesAll = allFields
	res.fieldNamesAnonymous = res.getFields(res.objType, fieldsAnonymous)
	res.fieldNamesFlattenAnonymous = res.getFields(res.objType, fieldsFlattenAnonymous)
	res.fieldNamesNoFlattenAnonymous = res.getFields(res.objType, fieldsNoFlattenAnonymous)

	res.methods = map[string]ObjMethodMetadata{}
	res.methodNames = []string{}

	if res.objKind != reflect.Invalid {
		res.fields = map[string]ObjFieldMetadata{}
		for _, fieldName := range allFields {
			res.fields[fieldName] = *newObjFieldMetadata(res.objType, fieldName, res)
		}
		for i := 0; i < res.objType.NumMethod(); i++ {
			method := res.objType.Method(i)
			res.methodNames = append(res.methodNames, method.Name)
			res.methods[method.Name] = *newObjMethodMetadata(res.objType, method.Name, res)
		}
	}

	return res
}

// IsStructOrPtrToStruct checks if the value is a struct or a pointer to a struct.
func (om *ObjMetadata) IsStructOrPtrToStruct() bool {
	return om.isStruct || om.isPtrToStruct
}

func (om *ObjMetadata) appendFields(fields []string, field reflect.StructField, listingType fieldListingType) []string {
	k := field.Type.Kind()
	if listingType == fieldsAnonymous {
		if field.Anonymous {
			fields = append(fields, field.Name)
		}
	} else if listingType == fieldsAll {
		fields = append(fields, field.Name)
		if k == reflect.Struct && field.Anonymous {
			fields = append(fields, om.getFields(field.Type, listingType)...)
		}
	} else {
		if listingType == fieldsFlattenAnonymous && k == reflect.Struct && field.Anonymous {
			fields = append(fields, om.getFields(field.Type, listingType)...)
		} else {
			fields = append(fields, field.Name)
		}
	}
	return fields
}

func (om *ObjMetadata) getFields(ty reflect.Type, listingType fieldListingType) []string {
	var fields []string

	if ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}

	if ty.Kind() != reflect.Struct {
		return fields // No need to populate nonstructs
	}

	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		fields = om.appendFields(fields, f, listingType)
	}

	return fields
}

// ObjFieldMetadata contains data which is always unique per Type/Field.
type ObjFieldMetadata struct {
	name string

	structField reflect.StructField

	// Valid here is not yet the final info about an actual field validity,
	// because value field still have .IsValid()
	valid bool

	fieldKind reflect.Kind
	fieldType reflect.Type
}

func newObjFieldMetadata(ty reflect.Type, name string, objMetadata *ObjMetadata) *ObjFieldMetadata {
	res := &ObjFieldMetadata{}
	res.fieldKind = reflect.Invalid
	res.name = name
	if objMetadata.IsStructOrPtrToStruct() {
		var found bool
		var structField reflect.StructField
		if objMetadata.isPtrToStruct {
			structField, found = objMetadata.objType.Elem().FieldByName(res.name)
		} else {
			structField, found = objMetadata.objType.FieldByName(res.name)
		}
		res.structField = structField
		res.fieldType = structField.Type
		if res.fieldType == nil {
			res.valid = false
		} else {
			res.fieldKind = structField.Type.Kind()
			res.valid = found
		}
	}
	return res
}

// ObjMethodMetadata contains data
// which is always unique per Type/Method.
type ObjMethodMetadata struct {
	name   string
	method reflect.Method
	valid  bool
}

func newObjMethodMetadata(ty reflect.Type, name string, objMetadata *ObjMetadata) *ObjMethodMetadata {
	res := &ObjMethodMetadata{name: name}

	if objMetadata.objKind == reflect.Invalid {
		res.valid = false
	} else {
		if method, found := objMetadata.objType.MethodByName(name); found {
			res.method = method
			res.valid = res.method.Func.IsValid()
		} else {
			res.valid = false
		}
	}

	return res
}

// Obj is a wrapper for golang values which need to be reflected.
// The value can be of any kind and any type.
type Obj struct {
	iface interface{}
	// Value used to work with fields. The only special case is when iface is a pointer to a struct, in
	// that case this is the value of that struct:
	fieldsValue reflect.Value
	ObjMetadata
}

// NewFromType creates a new Obj but using reflect.Type.
func NewFromType(ty reflect.Type) *Obj {
	if ty == nil {
		return New(nil)
	}
	return New(reflect.New(ty).Interface())
}

// New initializes a new Obj wrapper.
func New(obj interface{}) *Obj {
	o := &Obj{iface: obj}

	ty := reflect.TypeOf(obj)
	metadataCacheMutex.RLock()
	metadata, found := metadataCache[ty]
	metadataCacheMutex.RUnlock()
	if found {
		o.ObjMetadata = metadata
	} else {
		o.ObjMetadata = *newObjMetadata(reflect.TypeOf(obj))
		updateCache(ty, o)
	}

	o.fieldsValue = reflect.Indirect(reflect.ValueOf(obj))

	return o
}

// IsValid checks if the underlying objects is valid.
// Nil is an invalid value, for example.
func (o *Obj) IsValid() bool {
	return o.objKind != reflect.Invalid
}

// Len returns object length. Works for arrays, channels, maps, slices and strings.
//
// It doesn't panic for other types, returns 0 instead.
//
// In case the value is a pointer, len checks the underlying value.
func (o *Obj) Len() int {
	switch o.fieldsValue.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return o.fieldsValue.Len()
	}
	return 0
}

// IsGettableByIndex returns true if underlying type is array, slice or string
func (o *Obj) IsGettableByIndex() bool {
	switch o.fieldsValue.Kind() {
	case reflect.Array, reflect.Slice, reflect.String:
		return true
	default:
		return false
	}
}

// IsSettableByIndex returns true if underlying type is array, slice (but not string, since they are immutable)
func (o *Obj) IsSettableByIndex() bool {
	switch o.fieldsValue.Kind() {
	case reflect.Array, reflect.Slice:
		return true
	default:
		return false
	}
}

// IsMap returns true if underlying type is map or a pointer to a map
func (o *Obj) IsMap() bool {
	switch o.fieldsValue.Kind() {
	case reflect.Map:
		return true
	default:
		return false
	}
}

// GetByIndex returns a value by int index.
//
// Works for arrays, slices and strins. Won't panic when index or kind is invalid.
func (o *Obj) GetByIndex(index int) (value interface{}, found bool) {
	defer func() {
		if err := recover(); err != nil {
			value = nil
			found = false
		}
	}()

	if o.IsGettableByIndex() {
		if 0 <= index && index < o.fieldsValue.Len() {
			value = o.fieldsValue.Index(index).Interface()
			found = true
			return
		}
	}

	return
}

// SetByIndex sets a slice value by key.
func (o *Obj) SetByIndex(index int, val interface{}) error {
	if index < 0 || o.Len() <= index {
		return fmt.Errorf("cannot set element %d", index)
	}

	if o.IsSettableByIndex() {
		elem := o.fieldsValue.Index(index)
		elem.Set(reflect.ValueOf(val))
		return nil
	}

	return fmt.Errorf("cannot set element %d of %s", index, o.fieldsValue.String())
}

// Keys return map keys in unspecified order.
func (o *Obj) Keys() ([]interface{}, error) {
	if o.IsMap() {
		keys := o.fieldsValue.MapKeys()
		res := make([]interface{}, len(keys))
		for n := range keys {
			res[n] = keys[n].Interface()
		}
		return res, nil
	}
	return nil, fmt.Errorf("invalid type %s", o.Type().String())
}

// SetByKey sets a map value by key.
func (o *Obj) SetByKey(key interface{}, val interface{}) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("cannot set key %s: %v", key, e)
		}
	}()

	if o.IsMap() {
		o.fieldsValue.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(val))
		return
	}
	err = fmt.Errorf("cannot set key %s: %w", key, err)
	return
}

// GetByKey returns a value by map key.
//
// Won't panic when key is invalid or kind is not map.
func (o *Obj) GetByKey(key interface{}) (value interface{}, found bool) {
	defer func() {
		if err := recover(); err != nil {
			value = nil
			found = false
		}
	}()

	if o.IsMap() {
		v := o.fieldsValue.MapIndex(reflect.ValueOf(key))
		if !v.IsValid() {
			found = false
			return
		}
		value = v.Interface()
		found = true
		return
	}
	return
}

// Fields returns fields.
// Don't list fields inside Anonymous fields as distinct fields.
func (o *Obj) Fields() []ObjField {
	return o.getFields(fieldsNoFlattenAnonymous)
}

// FieldsFlattened returns fields.
// Will not list Anonymous fields but it will list fields declared in those anonymous fields.
func (o Obj) FieldsFlattened() []ObjField {
	return o.getFields(fieldsFlattenAnonymous)
}

// FieldsAll returns fields.
// List both anonymous fields and fields declared inside anonymous fields.
func (o Obj) FieldsAll() []ObjField {
	return o.getFields(fieldsAll)
}

// FieldsAnonymous returns only anonymous fields.
func (o Obj) FieldsAnonymous() []ObjField {
	return o.getFields(fieldsAnonymous)
}

func (o *Obj) getFields(listingType fieldListingType) []ObjField {
	var fieldNames []string
	switch listingType {
	case fieldsAll:
		fieldNames = o.fieldNamesAll
	case fieldsAnonymous:
		fieldNames = o.fieldNamesAnonymous
	case fieldsFlattenAnonymous:
		fieldNames = o.fieldNamesFlattenAnonymous
	case fieldsNoFlattenAnonymous:
		fieldNames = o.fieldNamesNoFlattenAnonymous
	default:
		panic(fmt.Sprintf("Invalid field listing type %d", listingType))
	}

	res := make([]ObjField, len(fieldNames))
	for n, fieldName := range fieldNames {
		res[n] = *o.Field(fieldName)
	}

	return res
}

// FindDoubleFields checks if this object has declared
// multiple fields with a same name.
// (by checking recursively Anonymous fields and their fields)
func (o Obj) FindDoubleFields() []string {
	fields := map[string]int{}
	res := []string{}
	for _, f := range o.FieldsAll() {
		counter := fields[f.name]
		if counter == 1 {
			res = append(res, f.name)
		}
		fields[f.name] = counter + 1
	}
	return res
}

// IsPtr checks if the value is a pointer.
func (o Obj) IsPtr() bool {
	return o.objKind == reflect.Ptr
}

func (o Obj) Dereferenced() interface{} {
	val := o.fieldsValue
	for val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	return val.Interface()
}

// Field get a field wrapper.
// Note that the field name can be invalid.
// You can check the field validity using ObjField.IsValid().
func (o *Obj) Field(fieldName string) *ObjField {
	if o.fieldsValue.IsValid() {
		if metadata, found := o.fields[fieldName]; found {
			return newObjField(o, metadata)
		}
	}
	return newObjField(o, ObjFieldMetadata{name: fieldName, valid: false, fieldKind: reflect.Invalid})
}

// Type returns the value type.
// If kind is invalid, this will return a zero filled reflect.Type.
func (o Obj) Type() reflect.Type {
	return o.objType
}

// Kind returns the value's kind.
func (o Obj) Kind() reflect.Kind {
	return o.objKind
}

func (o Obj) String() string {
	if o.objType == nil {
		return "nil"
	}
	return o.objType.String()
}

// Method returns a new method wrapper.
// The method name can be invalid, check the method validity with ObjMethod.IsValid().
func (o *Obj) Method(name string) *ObjMethod {
	if metadata, found := o.methods[name]; found {
		return newObjMethod(o, metadata)
	}
	return newObjMethod(o, ObjMethodMetadata{name: name, valid: false})
}

// Methods returns the list of all methods.
func (o *Obj) Methods() []ObjMethod {
	res := make([]ObjMethod, 0, len(o.methodNames))
	for _, name := range o.methodNames {
		res = append(res, *o.Method(name))
	}
	return res
}

// ObjField is a wrapper for the object's field.
type ObjField struct {
	obj   *Obj
	value reflect.Value

	ObjFieldMetadata
}

func newObjField(obj *Obj, metadata ObjFieldMetadata) *ObjField {
	res := &ObjField{
		obj:              obj,
		ObjFieldMetadata: metadata,
	}

	if metadata.valid && res.obj.IsStructOrPtrToStruct() {
		res.value = obj.fieldsValue.FieldByName(res.name)
	}

	return res
}

func (of *ObjField) assertValid() error {
	if !of.IsValid() {
		return fmt.Errorf("invalid field %s", of.name)
	}
	return nil
}

// IsValid checks if the fields is valid.
func (of *ObjField) IsValid() bool {
	return of.valid && of.value.IsValid()
}

// Name returns the field's name.
func (of *ObjField) Name() string {
	return of.name
}

// Kind returns the field's kind.
func (of *ObjField) Kind() reflect.Kind {
	return of.fieldKind
}

// Type returns the field's type.
func (of *ObjField) Type() reflect.Type {
	return of.fieldType
}

// Tag returns the value of this specific tag
// or error if the field is invalid.
func (of *ObjField) Tag(tag string) (string, error) {
	if err := of.assertValid(); err != nil {
		return "", err
	}
	return of.structField.Tag.Get(tag), nil
}

// Tags returns the map of all fields or error for invalid field.
func (of *ObjField) Tags() (map[string]string, error) {
	if err := of.assertValid(); err != nil {
		return nil, err
	}

	tag := of.structField.Tag
	return ParseTag(string(tag))
}

// TagsString returns the complete tags string (everything inside “)
func (of *ObjField) TagsString() (string, error) {
	if err := of.assertValid(); err != nil {
		return "", err
	}

	return string(of.structField.Tag), nil
}

// TagExpanded returns the tag value "expanded" with commas.
func (of *ObjField) TagExpanded(tag string) ([]string, error) {
	if err := of.assertValid(); err != nil {
		return nil, err
	}
	return strings.Split(of.structField.Tag.Get(tag), ","), nil
}

// IsAnonymous checks if this is an anonymous (embedded) field.
func (of *ObjField) IsAnonymous() bool {
	if err := of.assertValid(); err != nil {
		return false
	}
	field, found := of.obj.underlyingType.FieldByName(of.name)
	if !found {
		return false
	}
	return field.Anonymous
}

// IsExported returns true if the name starts with uppercase (i.e. field is public).
func (of *ObjField) IsExported() bool {
	return of.structField.PkgPath == ""
}

// IsSettable checks if this field is settable.
func (of *ObjField) IsSettable() bool {
	return of.value.CanSet()
}

// Set sets a value for this field or error if field is invalid (or not settable).
func (of *ObjField) Set(value interface{}) error {
	if err := of.assertValid(); err != nil {
		return err
	}

	if !of.IsSettable() {
		return fmt.Errorf("field %s in %T not settable", of.name, of.obj.iface)
	}

	of.value.Set(reflect.ValueOf(value))

	return nil
}

// Get gets the field value of error if field is invalid).
func (of *ObjField) Get() (interface{}, error) {
	if err := of.assertValid(); err != nil {
		return nil, err
	}
	if !of.IsExported() {
		return nil, fmt.Errorf("cannot read unexported field %T.%s", of.obj.iface, of.name)
	}

	return of.value.Interface(), nil
}

// ObjMethod is a wrapper for an object method.
// The name of the method can be invalid.
type ObjMethod struct {
	obj *Obj
	ObjMethodMetadata
}

func newObjMethod(obj *Obj, objMethodMetadata ObjMethodMetadata) *ObjMethod {
	return &ObjMethod{
		obj:               obj,
		ObjMethodMetadata: objMethodMetadata,
	}
}

// Name returns the method's name.
func (om *ObjMethod) Name() string {
	return om.name
}

const (
	onlyInTypes  = 0
	onlyOutTypes = 1
)

func (om *ObjMethod) methodTypes(kind int) []reflect.Type {
	m := reflect.ValueOf(om.obj.iface).MethodByName(om.name)
	if !m.IsValid() {
		return []reflect.Type{}
	}
	ty := m.Type()

	// inTypes are default
	tyNum := ty.NumIn()
	tyFn := ty.In
	if kind == onlyOutTypes {
		tyNum = ty.NumOut()
		tyFn = ty.Out
	}

	out := make([]reflect.Type, tyNum)
	for i := 0; i < tyNum; i++ {
		out[i] = tyFn(i)
	}
	return out
}

// InTypes returns an slice with this method's input types.
func (om *ObjMethod) InTypes() []reflect.Type {
	return om.methodTypes(onlyInTypes)
}

// OutTypes returns an slice with this method's output types.
func (om *ObjMethod) OutTypes() []reflect.Type {
	return om.methodTypes(onlyOutTypes)
}

// IsValid returns this method's validity.
func (om *ObjMethod) IsValid() bool {
	return om.valid
}

// Call calls this method.
// Note that in the error returning value is not the error from the method call.
func (om *ObjMethod) Call(args ...interface{}) (*CallResult, error) {
	if !om.obj.IsValid() {
		return nil, fmt.Errorf("invalid object type %T for method %s", om.obj.iface, om.name)
	}
	if !om.IsValid() {
		return nil, fmt.Errorf("invalid method %s in %T", om.name, om.obj.iface)
	}
	in := make([]reflect.Value, len(args)+1)
	in[0] = reflect.ValueOf(om.obj.iface)
	for n := range args {
		in[n+1] = reflect.ValueOf(args[n])
	}
	out := om.method.Func.Call(in)
	res := make([]interface{}, len(out))
	for n := range out {
		res[n] = out[n].Interface()
	}
	return newCallResult(res), nil
}

// CallResult is a wrapper of a method call result.
type CallResult struct {
	Result []interface{}
	Error  error
}

func newCallResult(res []interface{}) *CallResult {
	cr := &CallResult{Result: res}
	if len(res) == 0 {
		return cr
	}
	errorCandidate := res[len(res)-1]
	if errorCandidate != nil {
		if err, is := errorCandidate.(error); is {
			cr.Error = err
		}
	}
	return cr
}

// IsError checks if the last value is a non-nil error.
func (cr *CallResult) IsError() bool {
	return cr.Error != nil
}
