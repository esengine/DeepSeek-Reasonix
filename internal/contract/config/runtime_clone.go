package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
)

// CloneResolved retains resolved credentials and source authority without rereading disk.
// The clone owns its mutable configuration collections and preserves their Go types.
func (c *Config) CloneResolved() (*Config, error) {
	if c == nil {
		return nil, nil
	}
	value, err := cloneConfigValue(reflect.ValueOf(*c), 0)
	if err != nil {
		return nil, err
	}
	copy := value.Interface().(Config)
	copy.providerSources = maps.Clone(c.providerSources)
	copy.expansionEnv = maps.Clone(c.expansionEnv)
	copy.pluginPackageOwners = maps.Clone(c.pluginPackageOwners)
	copy.pluginPackageSkillOwners = cloneConfigLists(c.pluginPackageSkillOwners)
	copy.pluginPackageAgentOwners = cloneConfigLists(c.pluginPackageAgentOwners)
	copy.explicitProjectSkillKeys = maps.Clone(c.explicitProjectSkillKeys)
	copy.explicitProjectProviderNames = maps.Clone(c.explicitProjectProviderNames)
	copy.loadWarnings = slices.Clone(c.loadWarnings)
	copy.projectScope = cloneProjectScope(c.projectScope)
	copy.shadowedProjectProviders, err = cloneResolvedProviders(c.shadowedProjectProviders)
	if err != nil {
		return nil, err
	}
	copy.projectProviderEditBaseline, err = cloneResolvedProviders(c.projectProviderEditBaseline)
	if err != nil {
		return nil, err
	}
	for i := range copy.Providers {
		cloneProviderPrivate(&copy.Providers[i], c.Providers[i])
	}
	for i := range copy.Plugins {
		copy.Plugins[i].expansionEnv = maps.Clone(c.Plugins[i].expansionEnv)
	}
	return &copy, nil
}

func cloneConfigLists(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = slices.Clone(v)
	}
	return out
}

func cloneProjectScope(scope projectScopeReport) projectScopeReport {
	scope.ignored = slices.Clone(scope.ignored)
	scope.extraRoots = slices.Clone(scope.extraRoots)
	scope.pending = slices.Clone(scope.pending)
	scope.admitted = slices.Clone(scope.admitted)
	for i := range scope.pending {
		scope.pending[i].Files = slices.Clone(scope.pending[i].Files)
	}
	for i := range scope.admitted {
		scope.admitted[i].Files = slices.Clone(scope.admitted[i].Files)
	}
	return scope
}

func cloneProviderPrivate(target *ProviderEntry, source ProviderEntry) {
	if source.visionOverride != nil {
		value := *source.visionOverride
		target.visionOverride = &value
	}
}

func cloneResolvedProviders(entries []ProviderEntry) ([]ProviderEntry, error) {
	value, err := cloneConfigValue(reflect.ValueOf(entries), 0)
	if err != nil {
		return nil, err
	}
	out := value.Interface().([]ProviderEntry)
	for i := range out {
		cloneProviderPrivate(&out[i], entries[i])
	}
	return out, nil
}

func cloneConfigValue(value reflect.Value, depth int) (reflect.Value, error) {
	if depth > 100 {
		return reflect.Value{}, fmt.Errorf("configuration contains a cycle or excessive nesting")
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		if value.IsNil() {
			return value, nil
		}
	}
	out := reflect.New(value.Type()).Elem()
	out.Set(value)
	switch value.Kind() {
	case reflect.Struct:
		return cloneConfigStruct(out, value, depth)
	case reflect.Pointer:
		out.Set(reflect.New(value.Type().Elem()))
		if err := assignConfigValue(out.Elem(), value.Elem(), depth); err != nil {
			return reflect.Value{}, err
		}
	case reflect.Interface:
		if err := assignConfigValue(out, value.Elem(), depth); err != nil {
			return reflect.Value{}, err
		}
	case reflect.Slice:
		out.Set(reflect.MakeSlice(value.Type(), value.Len(), value.Len()))
		return cloneConfigElements(out, value, depth)
	case reflect.Map:
		return cloneConfigMap(value, depth)
	case reflect.Array:
		return cloneConfigElements(out, value, depth)
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return reflect.Value{}, fmt.Errorf("configuration contains unsupported runtime owner %s", value.Type())
	}
	return out, nil
}

func assignConfigValue(dst, src reflect.Value, depth int) error {
	child, err := cloneConfigValue(src, depth+1)
	if err == nil {
		dst.Set(child)
	}
	return err
}

func cloneConfigStruct(out, value reflect.Value, depth int) (reflect.Value, error) {
	for i := range value.NumField() {
		if value.Type().Field(i).IsExported() {
			if err := assignConfigValue(out.Field(i), value.Field(i), depth); err != nil {
				return reflect.Value{}, err
			}
		}
	}
	return out, nil
}

func cloneConfigElements(out, value reflect.Value, depth int) (reflect.Value, error) {
	for i := range value.Len() {
		if err := assignConfigValue(out.Index(i), value.Index(i), depth); err != nil {
			return reflect.Value{}, err
		}
	}
	return out, nil
}

func cloneConfigMap(value reflect.Value, depth int) (reflect.Value, error) {
	out := reflect.MakeMapWithSize(value.Type(), value.Len())
	iter := value.MapRange()
	for iter.Next() {
		child, err := cloneConfigValue(iter.Value(), depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		out.SetMapIndex(iter.Key(), child)
	}
	return out, nil
}
