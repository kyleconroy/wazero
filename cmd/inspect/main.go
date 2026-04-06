package main

import (
	"fmt"
	"os"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	compbin "github.com/tetratelabs/wazero/internal/component/binary"
	binaryformat "github.com/tetratelabs/wazero/internal/wasm/binary"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	comp, err := compbin.DecodeComponent(data)
	if err != nil {
		panic(err)
	}
	for i, cm := range comp.CoreModules {
		mod, err := binaryformat.DecodeModule(cm.Data, api.CoreFeaturesV2|experimental.CoreFeaturesThreads, 65536, false, false, false)
		if err != nil {
			fmt.Printf("Module %d: decode error: %v\n", i, err)
			continue
		}
		fmt.Printf("=== Module %d imports ===\n", i)
		for _, imp := range mod.ImportSection {
			if imp.Type == 0 { // func
				ft := mod.TypeSection[imp.DescFunc]
				fmt.Printf("  [func] %s :: %s  params=%v results=%v\n", imp.Module, imp.Name, ft.Params, ft.Results)
			}
		}
	}
}
