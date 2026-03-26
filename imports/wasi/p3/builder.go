package p3

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Builder configures the WASI P3 host modules for later instantiation.
type Builder interface {
	// Instantiate registers all WASI P3 host modules into the runtime.
	Instantiate(context.Context) (api.Closer, error)
}

// NewBuilder returns a new Builder for configuring WASI P3 host modules.
func NewBuilder(r wazero.Runtime) Builder {
	return &builder{r: r}
}

type builder struct {
	r wazero.Runtime
}

func (b *builder) Instantiate(ctx context.Context) (api.Closer, error) {
	// Register the wasi:cli/environment host module (args + env vars).
	envMod, err := b.instantiateEnvironment(ctx)
	if err != nil {
		return nil, err
	}

	// Register the wasi:cli/stdin, stdout, stderr host modules.
	_, err = b.instantiateStdio(ctx)
	if err != nil {
		return nil, err
	}

	// Register the wasi:cli/exit host module.
	_, err = b.instantiateExit(ctx)
	if err != nil {
		return nil, err
	}

	// Register the wasi:clocks host modules.
	_, err = b.instantiateClocks(ctx)
	if err != nil {
		return nil, err
	}

	// Register the wasi:random host module.
	_, err = b.instantiateRandom(ctx)
	if err != nil {
		return nil, err
	}

	return envMod, nil
}
