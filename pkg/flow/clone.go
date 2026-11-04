package flow

import "reflect"

// CloneStep returns a deep copy of step: no pointer, slice, map or interface
// in the copy is shared with step, so the copy can be rewritten (variable
// expansion, default appIds) while step stays the parsed template that every
// later execution, such as the next repeat iteration, starts from.
func CloneStep(step Step) Step {
	if step == nil {
		return nil
	}
	return deepCopy(reflect.ValueOf(step)).Interface().(Step)
}

func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Elem().Type())
		c.Elem().Set(deepCopy(v.Elem()))
		return c
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		c.Set(v) // unexported fields keep their (value-copied) contents
		for i := 0; i < v.NumField(); i++ {
			if c.Field(i).CanSet() {
				c.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			c.Index(i).Set(deepCopy(v.Index(i)))
		}
		return c
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, k := range v.MapKeys() {
			c.SetMapIndex(k, deepCopy(v.MapIndex(k)))
		}
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(deepCopy(v.Elem()))
		return c
	default:
		return v
	}
}
