package reflector

import "reflect"

func Walk(i interface{}, f func(i2 interface{})) {
	ty := reflect.TypeOf(i)
	if ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}
}
