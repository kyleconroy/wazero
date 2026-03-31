package wasip2

import (
	"context"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/internal/wasm"
)

// wasi:filesystem/types function names.
const (
	fsReadViaStreamName         = "read-via-stream"
	fsWriteViaStreamName        = "write-via-stream"
	fsAppendViaStreamName       = "append-via-stream"
	fsAdviseName                = "advise"
	fsSyncDataName              = "sync-data"
	fsGetFlagsName              = "get-flags"
	fsGetTypeName               = "get-type"
	fsSetSizeName               = "set-size"
	fsSetTimesName              = "set-times"
	fsReadName                  = "read"
	fsWriteName                 = "write"
	fsReadDirectoryName         = "read-directory"
	fsSyncName                  = "sync"
	fsCreateDirectoryAtName     = "create-directory-at"
	fsStatName                  = "stat"
	fsStatAtName                = "stat-at"
	fsSetTimesAtName            = "set-times-at"
	fsLinkAtName                = "link-at"
	fsOpenAtName                = "open-at"
	fsReadlinkAtName            = "readlink-at"
	fsRemoveDirectoryAtName     = "remove-directory-at"
	fsRenameAtName              = "rename-at"
	fsSymlinkAtName             = "symlink-at"
	fsUnlinkFileAtName          = "unlink-file-at"
	fsIsSameObjectName          = "is-same-object"
	fsMetadataHashName          = "metadata-hash"
	fsMetadataHashAtName        = "metadata-hash-at"
	fsDropDescriptorName        = "drop-descriptor"
	fsReadDirectoryEntryName    = "read-directory-entry"
	fsDropDirectoryEntryStream  = "drop-directory-entry-stream"
)

// wasi:filesystem/preopens function names.
const (
	preopensGetDirectoriesName = "get-directories"
)

// FilesystemGetType implements wasi:filesystem/types.descriptor.get-type.
var FilesystemGetType = &wasm.HostFunc{
	ExportName:  fsGetTypeName,
	Name:        FilesystemTypesName + "#" + fsGetTypeName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "value"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		// tag=0 means ok.
		stack[0] = 0
		stack[1] = uint64(DescriptorTypeDirectory)
	})},
}

// FilesystemStat implements wasi:filesystem/types.descriptor.stat.
var FilesystemStat = &wasm.HostFunc{
	ExportName:  fsStatName,
	Name:        FilesystemTypesName + "#" + fsStatName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "result_ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// FilesystemStatAt implements wasi:filesystem/types.descriptor.stat-at.
var FilesystemStatAt = &wasm.HostFunc{
	ExportName:  fsStatAtName,
	Name:        FilesystemTypesName + "#" + fsStatAtName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "flags", "path_ptr", "path_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "result_ptr"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// FilesystemOpenAt implements wasi:filesystem/types.descriptor.open-at.
var FilesystemOpenAt = &wasm.HostFunc{
	ExportName:  fsOpenAtName,
	Name:        FilesystemTypesName + "#" + fsOpenAtName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "flags", "path_ptr", "path_len", "open_flags", "descriptor_flags"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "descriptor"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		// Return error: unsupported for now.
		stack[0] = 1 // error tag
		stack[1] = uint64(ErrorCodeUnsupported)
	})},
}

// FilesystemReadDirectory implements wasi:filesystem/types.descriptor.read-directory.
var FilesystemReadDirectory = &wasm.HostFunc{
	ExportName:  fsReadDirectoryName,
	Name:        FilesystemTypesName + "#" + fsReadDirectoryName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// FilesystemCreateDirectoryAt implements wasi:filesystem/types.descriptor.create-directory-at.
var FilesystemCreateDirectoryAt = &wasm.HostFunc{
	ExportName:  fsCreateDirectoryAtName,
	Name:        FilesystemTypesName + "#" + fsCreateDirectoryAtName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "path_ptr", "path_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 1 // error
	})},
}

// FilesystemRemoveDirectoryAt implements wasi:filesystem/types.descriptor.remove-directory-at.
var FilesystemRemoveDirectoryAt = &wasm.HostFunc{
	ExportName:  fsRemoveDirectoryAtName,
	Name:        FilesystemTypesName + "#" + fsRemoveDirectoryAtName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "path_ptr", "path_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 1 // error
	})},
}

// FilesystemUnlinkFileAt implements wasi:filesystem/types.descriptor.unlink-file-at.
var FilesystemUnlinkFileAt = &wasm.HostFunc{
	ExportName:  fsUnlinkFileAtName,
	Name:        FilesystemTypesName + "#" + fsUnlinkFileAtName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "path_ptr", "path_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 1 // error
	})},
}

// FilesystemRenameAt implements wasi:filesystem/types.descriptor.rename-at.
var FilesystemRenameAt = &wasm.HostFunc{
	ExportName:  fsRenameAtName,
	Name:        FilesystemTypesName + "#" + fsRenameAtName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
	ParamNames:  []string{"self", "old_path_ptr", "old_path_len", "new_desc", "new_path_ptr", "new_path_len"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32},
	ResultNames: []string{"tag"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 1 // error
	})},
}

// FilesystemReadViaStream implements wasi:filesystem/types.descriptor.read-via-stream.
var FilesystemReadViaStream = &wasm.HostFunc{
	ExportName:  fsReadViaStreamName,
	Name:        FilesystemTypesName + "#" + fsReadViaStreamName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI64},
	ParamNames:  []string{"self", "offset"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// FilesystemWriteViaStream implements wasi:filesystem/types.descriptor.write-via-stream.
var FilesystemWriteViaStream = &wasm.HostFunc{
	ExportName:  fsWriteViaStreamName,
	Name:        FilesystemTypesName + "#" + fsWriteViaStreamName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI64},
	ParamNames:  []string{"self", "offset"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// FilesystemAppendViaStream implements wasi:filesystem/types.descriptor.append-via-stream.
var FilesystemAppendViaStream = &wasm.HostFunc{
	ExportName:  fsAppendViaStreamName,
	Name:        FilesystemTypesName + "#" + fsAppendViaStreamName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "stream"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// FilesystemGetFlags implements wasi:filesystem/types.descriptor.get-flags.
var FilesystemGetFlags = &wasm.HostFunc{
	ExportName:  fsGetFlagsName,
	Name:        FilesystemTypesName + "#" + fsGetFlagsName,
	ParamTypes:  []api.ValueType{wasm.ValueTypeI32},
	ParamNames:  []string{"self"},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"tag", "flags"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		stack[0] = 0
		stack[1] = 0
	})},
}

// PreopensGetDirectories implements wasi:filesystem/preopens.get-directories.
var PreopensGetDirectories = &wasm.HostFunc{
	ExportName:  preopensGetDirectoriesName,
	Name:        FilesystemPreopensName + "#" + preopensGetDirectoriesName,
	ParamTypes:  []api.ValueType{},
	ParamNames:  []string{},
	ResultTypes: []api.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
	ResultNames: []string{"ptr", "len"},
	Code: wasm.Code{GoFunc: api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		// Return empty list for now.
		stack[0] = 0
		stack[1] = 0
	})},
}

