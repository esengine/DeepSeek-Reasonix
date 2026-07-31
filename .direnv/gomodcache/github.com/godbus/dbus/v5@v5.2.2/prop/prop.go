// Package prop provides the Properties struct which can be used to implement
// org.freedesktop.DBus.Properties.
package prop

import (
	"reflect"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// EmitType controls how org.freedesktop.DBus.Properties.PropertiesChanged is
// emitted for a property. If it is EmitTrue, the signal is emitted. If it is
// EmitInvalidates, the signal is also emitted, but the new value of the property
// is not disclosed. If it is EmitConst, the property never changes value during
// the lifetime of the object it belongs to, and hence the signal is never emitted
// for it.
type EmitType byte

const (
	EmitFalse EmitType = iota
	EmitTrue
	EmitInvalidates
	EmitConst
)

func (e EmitType) String() (str string) {
	switch e {
	case EmitFalse:
		str = "false"
	case EmitTrue:
		str = "true"
	case EmitInvalidates:
		str = "invalidates"
	case EmitConst:
		str = "const"
	default:
		panic("invalid value for EmitType")
	}
	return
}

// ErrIfaceNotFound is the error returned to peers who try to access properties
// on interfaces that aren't found.
var ErrIfaceNotFound = dbus.NewError("org.freedesktop.DBus.Properties.Error.InterfaceNotFound", nil)

// ErrPropNotFound is the error returned to peers trying to access properties
// that aren't found.
var ErrPropNotFound = dbus.NewError("org.freedesktop.DBus.Properties.Error.PropertyNotFound", nil)

// ErrReadOnly is the error returned to peers trying to set a read-only
// property.
var ErrReadOnly = dbus.NewError("org.freedesktop.DBus.Properties.Error.ReadOnly", nil)

// ErrInvalidArg is returned to peers if the type of the property that is being
// changed and the argument don't match.
var ErrInvalidArg = dbus.NewError("org.freedesktop.DBus.Properties.Error.InvalidArg", nil)

// The introspection data for the org.freedesktop.DBus.Properties interface.
var IntrospectData = introspect.Interface{
	Name: "org.freedesktop.DBus.Properties",
	Methods: []introspect.Method{
		{
			Name: "Get",
			Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "in"},
				{Name: "property", Type: "s", Direction: "in"},
				{Name: "value", Type: "v", Direction: "out"},
			},
		},
		{
			Name: "GetAll",
			Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "in"},
				{Name: "props", Type: "a{sv}", Direction: "out"},
			},
		},
		{
			Name: "Set",
			Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "in"},
				{Name: "property", Type: "s", Direction: "in"},
				{Name: "value", Type: "v", Direction: "in"},
			},
		},
	},
	Signals: []introspect.Signal{
		{
			Name: "PropertiesChanged",
			Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "out"},
				{Name: "changed_properties", Type: "a{sv}", Direction: "out"},
				{Name: "invalidates_properties", Type: "as", Direction: "out"},
			},
		},
	},
}

// The introspection data for the org.freedesktop.DBus.Properties interface, as
// a string.
const IntrospectDataString = `
	<interface name="org.freedesktop.DBus.Properties">
		<method name="Get">
			<arg name="interface" direction="in" type="s"/>
			<arg name="property" direction="in" type="s"/>
			<arg name="value" direction="out" type="v"/>
		</method>
		<method name="GetAll">
			<arg name="interface" direction="in" type="s"/>
			<arg name="props" direction="out" type="a{sv}"/>
		</method>
		<method name="Set">
			<arg name="interface" direction="in" type="s"/>
			<arg name="property" direction="in" type="s"/>
			<arg name="value" direction="in" type="v"/>
		</method>
		<signal name="PropertiesChanged">
			<arg name="interface" type="s"/>
			<arg name="changed_properties" type="a{sv}"/>
			<arg name="invalidates_properties" type="as"/>
		</signal>
	</interface>
`

// Prop represents a single property. It is used for creating a Properties
// value.
type Prop struct {
	// Initial value. Must be a DBus-representable type. This is not modified
	// after Properties has been initialized; use Get or GetMust to access the
	// value.
	Value any

	// If true, the value can be modified by calls to Set.
	Writable bool

	// Controls how org.freedesktop.DBus.Properties.PropertiesChanged is
	// emitted if this property changes.
	Emit EmitType

	// If not nil, anytime this property is changed by Set, this function is
	// called with an appropriate Change as its argument. If the returned error
	// is not nil, it is sent back to the caller of Set and the property is not
	// changed.
	Callback func(*Change) *dbus.Error
}

// Introspection returns the introspection data for p.
// The "name" argument is used as the property's name in the resulting data.
func (p *Prop) Introspection(name string) introspect.Property {
	result := introspect.Property{Name: name, Type: dbus.SignatureOf(p.Value).String()}
	if p.Writable {
		result.Access = "readwrite"
	} else {
		result.Access = "read"
	}
	result.Annotations = []introspect.Annotation{
		{
			Name:  "org.freedesktop.DBus.Property.EmitsChangedSignal",
			Value: p.Emit.String(),
		},
	}
	return result
}

// Change represents a change of a property by a call to Set.
type Change struct {
	Props *Properties
	Iface string
	Name  string
	Value any
}

// Properties is a set of values that can be made available to the message bus
// using the org.freedesktop.DBus.Properties interface. It is safe for
// concurrent use by multiple goroutines.
type Properties struct {
	m    Map
	mut  sync.RWMutex
	conn *dbus.Conn
	path dbus.ObjectPath
}

// New falls back to Export, but it returns nil if properties export fails,
// swallowing the error, shouldn't be used.
//
// Deprecated: use Export instead.
func New(conn *dbus.Conn, path dbus.ObjectPath, props Map) *Properties {
	p, err := Export(conn, path, props)
	if err != nil {
		return nil
	}
	return p
}

// Export returns a new Properties structure that manages the given properties.
// The key for the first-level map of props is the name of the interface; the
// second-level key is the name of the property. The returned structure will be
// exported as org.freedesktop.DBus.Properties on path.
func Export(
	conn *dbus.Conn, path dbus.ObjectPath, props Map,
) (*Properties, error) {
	p := &Properties{m: copyProps(props), conn: conn, path: path}
	if err := conn.Export(p, path, "org.freedesktop.DBus.Properties"); err != nil {
		return nil, err
	}
	return p, nil
}

// Map is a helper type for supplying the configuration of properties to be handled.
type Map = map[string]map[string]*Prop

func copyProps(in Map) Map {
	out := make(Map, len(in))
	for intf, props := range in {
		out[intf] = make(map[string]*Prop)
		for name, prop := range props {
			out[intf][name] = new(Prop)
			*out[intf][name] = *prop
			val := reflect.New(reflect.TypeOf(prop.Value))
			val.Elem().Set(reflect.ValueOf(prop.Value))
			out[intf][name].Value = val.Interface()
		}
	}
	return out
}

// Get implements org.freedesktop.DBus.Properties.Get.
func (p *Properties) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	p.mut.RLock()
	defer p.mut.RUnlock()
	m, ok := p.m[iface]
	if !ok {
		return dbus.Variant{}, ErrIfaceNotFound
	}
	prop, ok := m[property]
	if !ok {
		return dbus.Variant{}, ErrPropNotFound
	}
	return dbus.MakeVariant(reflect.ValueOf(prop.Value).Elem().Interface()), nil
}

// GetAll implements org.freedesktop.DBus.Properties.GetAll.
func (p *Properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	p.mut.RLock()
	defer p.mut.RUnlock()
	m, ok := p.m[iface]
	if !ok {
		return nil, ErrIfaceNotFound
	}
	rm := make(map[string]dbus.Variant, len(m))
	for k, v := range m {
		rm[k] = dbus.MakeVariant(reflect.ValueOf(v.Value).Elem().Interface())
	}
	return rm, nil
}

// GetMust returns the value of the given property and panics if either the
// interface or the property name are invalid.
func (p *Properties) GetMust(iface, property string) any {
	p.mut.RLock()
	defer p.mut.RUnlock()
	return reflect.ValueOf(p.m[iface][property].Value).Elem().Interface()
}

// Introspection returns the introspection data that represents the properties
// of iface.
func (p *Properties) Introspection(iface string) []introspect.Property {
	p.mut.RLock()
	defer p.mut.RUnlock()
	m := p.m[iface]
	s := make([]introspect.Property, 0, len(m))
	for name, prop := range m {
		s = append(s, prop.Introspection(name))
	}
	return s
}

// set sets the given property and emits PropertyChanged if appropriate. p.mut
// must already be locked.
func (p *Properties) set(iface, property string, v any) error {
	prop := p.m[iface][property]
	err := dbus.Store([]any{v}, prop.Value)
	if err != nil {
		return err
	}
	return p.emitChange(iface, property)
}

func (p *Properties) emitChange(iface, property string) error {
	prop := p.m[iface][property]
	switch prop.Emit {
	case EmitFalse:
		return nil // do nothing
	case EmitInvalidates:
		return p.conn.Emit(p.path, "org.freedesktop.DBus.Properties.PropertiesChanged",
			iface, map[string]dbus.Variant{}, []string{property})
	case EmitTrue:
		return p.conn.Emit(p.path, "org.freedesktop.DBus.Properties.PropertiesChanged",
			iface, map[string]dbus.Variant{property: dbus.MakeVariant(prop.Value)},
			[]string{})
	case EmitConst:
		return nil
	default:
		panic("invalid value for EmitType")
	}
}

// Set implements org.freedesktop.Properties.Set.
func (p *Properties) Set(iface, property string, newv dbus.Variant) *dbus.Error {
	p.mut.Lock()
	defer p.mut.Unlock()
	m, ok := p.m[iface]
	if !ok {
		return ErrIfaceNotFound
	}
	prop, ok := m[property]
	if !ok {
		return ErrPropNotFound
	}
	if !prop.Writable {
		return ErrReadOnly
	}
	if newv.Signature() != dbus.SignatureOf(prop.Value) {
		return ErrInvalidArg
	}
	if prop.Callback != nil {
		err := prop.Callback(&Change{p, iface, property, newv.Value()})
		if err != nil {
			return err
		}
	}
	if err := p.set(iface, property, newv.Value()); err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

// SetMust sets the value of the given property and panics if the interface or
// the property name are invalid.
func (p *Properties) SetMust(iface, property string, v any) {
	p.mut.Lock()
	defer p.mut.Unlock() // unlock in case of panic
	err := p.set(iface, property, v)
	if err != nil {
		panic(err)
	}
}
