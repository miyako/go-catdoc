package gocatdoc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"os"

	"embed"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/emscripten"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

//go:embed catdoc.wasm
var binary []byte

//go:embed charsets/*
var charsets embed.FS

var runtimeConfig wazero.RuntimeConfig
var r wazero.Runtime
var compiledModule wazero.CompiledModule
var ctx context.Context
var initLock = &sync.Mutex{}

func getWASMModuleWithFS(file fs.FS, stdout, stderr io.Writer) (api.Module, error) {
	cMod, run, err := getCompiledWASMModule()
	if err != nil {
		return nil, err
	}

	s := os.Getenv("CATDOC_SRC_CHARSET")
	d := os.Getenv("CATDOC_DST_CHARSET")

	mod, err := run.InstantiateModule(ctx, cMod, wazero.NewModuleConfig().
		WithStartFunctions("_initialize").
		WithEnv("CATDOC_SRC_CHARSET", s).
		WithEnv("CATDOC_DST_CHARSET", d).
		WithFSConfig(
			wazero.NewFSConfig().
				WithFSMount(file, "/input_file/").
				WithFSMount(charsets, "/charsets/")).
		WithStdout(stdout).
		WithStderr(stderr))

	return mod, err
}

func getCompiledWASMModule() (wazero.CompiledModule, wazero.Runtime, error) {
	initLock.Lock()
	defer initLock.Unlock()

	if r == nil {
		ctx = context.Background()

		if runtimeConfig == nil {
			cache := wazero.NewCompilationCache()
			runtimeConfig = wazero.NewRuntimeConfig().WithCompilationCache(cache)
		}

		r = wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

		// 1. Instantiate WASI (always needed for Emscripten modules)
		wasi_snapshot_preview1.MustInstantiate(ctx, r)

		// 2. Compile the WASM module first (needed to inspect imports)
		module, err := r.CompileModule(ctx, binary)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to compile WASM module: %w", err)
		}
		compiledModule = module

		// 3. Build "env" module with Emscripten imports + syscall stubs
		envBuilder := r.NewHostModuleBuilder("env")

		// ✅ Stub __syscall_faccessat to always succeed
		envBuilder.NewFunctionBuilder().
			WithFunc(func(dirfd, pathname, mode, flags uint32) int32 {
				return 0 // always accessible
			}).
			Export("__syscall_faccessat")

		envBuilder.NewFunctionBuilder().
		WithFunc(func(fd, direntPtr, count uint32) int32 {
			return 0
		}).
		Export("__syscall_getdents64")
		
		envBuilder.NewFunctionBuilder().
		WithFunc(func(dirfd, pathname, flags uint32) int32 {
			return 0 // pretend unlinkat succeeded
		}).
		Export("__syscall_unlinkat")

		// ✅ Add required Emscripten functions for this WASM module
		exporter, err := emscripten.NewFunctionExporterForModule(compiledModule)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get Emscripten exporter: %w", err)
		}
		exporter.ExportFunctions(envBuilder)

		// 4. Instantiate the env module
		if _, err := envBuilder.Instantiate(ctx); err != nil {
			return nil, nil, fmt.Errorf("failed to instantiate env module: %w", err)
		}

		// Now ready to instantiate actual guest module using this runtime
	}

	return compiledModule, r, nil
}

func GetTextFromFile(file io.ReadSeeker) (string, error) {
	return callWASMFuncWithFile("get_text", file)
}

func GetTitleFromFile(file io.ReadSeeker) (string, error) {
	return callWASMFuncWithFile("get_title", file)
}

func GetSubjectFromFile(file io.ReadSeeker) (string, error) {
	return callWASMFuncWithFile("get_subject", file)
}

func GetKeywordsFromFile(file io.ReadSeeker) (string, error) {
	return callWASMFuncWithFile("get_keywords", file)
}

func GetCommentsFromFile(file io.ReadSeeker) (string, error) {
	return callWASMFuncWithFile("get_comments", file)
}

func GetAnnotationAuthorsFromFile(file io.ReadSeeker) ([]string, error) {
	r, err := callWASMFuncWithFile("get_annotation_authors", file)
	if err != nil {
		return nil, err
	}
	return strings.Split(r, "\n"), nil
}

func GetVersion() (string, error) {
	return callWASMFunc("get_version", nil)
}

func callWASMFuncWithFile(funcName string, file io.ReadSeeker) (string, error) {
	fileFS, err := newFakeFS(file)
	if err != nil {
		return "", err
	}

	return callWASMFunc(funcName, fileFS)
}

func callWASMFunc(funcName string, fs fs.FS) (string, error) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	mod, err := getWASMModuleWithFS(fs, outBuf, errBuf)
	if err != nil {
		return "", fmt.Errorf("could not get wasm module: %w", err)
	}
	_, err = mod.ExportedFunction(funcName).Call(ctx)
	if err != nil {
		if exitError, ok := err.(*sys.ExitError); ok && exitError.ExitCode() != 0 {
			return "", fmt.Errorf("%s %w", errBuf.String(), exitError)
		}
	}

	outStr := outBuf.String()
	errStr := errBuf.String()
	outStr = strings.TrimRight(outStr, "\n")
	errStr = strings.TrimRight(errStr, "\n")
	err = nil
	if errStr != "" {
		err = fmt.Errorf(errStr)
	}
	return outStr, err
}
