package js

import (
	"context"
	"fmt"

	"github.com/dop251/goja"

	"github.com/docker/cagent/pkg/environment"
)

type jsEnv func(string) goja.Value

func (e jsEnv) Get(k string) goja.Value     { return e(k) }
func (e jsEnv) Set(string, goja.Value) bool { return false }
func (e jsEnv) Has(string) bool             { return true }
func (e jsEnv) Delete(string) bool          { return true }
func (e jsEnv) Keys() []string              { return nil }

func Expand(ctx context.Context, kv map[string]string, env environment.Provider) map[string]string {
	expanded := map[string]string{}

	vm := goja.New()
	_ = vm.Set("env", vm.NewDynamicObject(jsEnv(func(k string) goja.Value {
		return vm.ToValue(env.Get(ctx, k))
	})))

	for k, v := range kv {
		result, err := vm.RunString("`" + v + "`")
		if err != nil {
			expanded[k] = v
			continue
		}

		expanded[k] = fmt.Sprintf("%v", result.Export())
	}

	return expanded
}

func ExpandString(ctx context.Context, str string, values map[string]string) (string, error) {
	vm := goja.New()

	for k, v := range values {
		_ = vm.Set(k, v)
	}

	expanded, err := vm.RunString("`" + str + "`")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%v", expanded.Export()), nil
}
