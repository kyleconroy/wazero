package p2

import (
	"context"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// instantiateExit registers the wasi:cli/exit@0.2.0 host module.
func (b *builder) instantiateExit(ctx context.Context) (api.Closer, error) {
	return b.r.NewHostModuleBuilder(CliExit).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(exit), []api.ValueType{i32}, nil).
		WithName("exit").
		Export("exit").
		Instantiate(ctx)
}

// exit implements wasi:cli/exit.exit.
func exit(_ context.Context, _ api.Module, stack []uint64) {
	status := api.DecodeU32(stack[0])
	exitCode := uint32(0)
	if status != 0 {
		exitCode = 1
	}
	panic(sys.NewExitError(exitCode))
}
