package gocatdoc

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"

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

var (
	runtimeConfig   wazero.RuntimeConfig
	r               wazero.Runtime
	compiledModule  wazero.CompiledModule
	ctx             context.Context
	initLock        = &sync.Mutex{}
)

// GetTextFromFile returns the plain text from a Word document.
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

	outStr := strings.TrimRight(outBuf.String(), "\n")
	errStr := strings.TrimRight(errBuf.String(), "\n")

	if errStr != "" {
		return outStr, fmt.Errorf(errStr)
	}

	return outStr, nil
}

func getWASMModuleWithFS(file fs.FS, stdout, stderr io.Writer) (api.Module, error) {
	cMod, run, err := getCompiledWASMModule()
	if err != nil {
		return nil, err
	}

	// Charset env config
	srcCharset := os.Getenv("CATDOC_SRC_CHARSET")
	dstCharset := os.Getenv("CATDOC_DST_CHARSET")

	// 👇 Match what your C code expects (e.g., -DCHARSETPATH="charsets")
	return run.InstantiateModule(ctx, cMod, wazero.NewModuleConfig().
		WithStartFunctions("_initialize").
		WithEnv("CATDOC_SRC_CHARSET", srcCharset).
		WithEnv("CATDOC_DST_CHARSET", dstCharset).
		WithEnv("CHARSETPATH", "/charsets").
		WithFSConfig(
			wazero.NewFSConfig().
				WithFSMount(file, "/input_file"),
		).
		WithStdout(stdout).
		WithStderr(stderr),
	)
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

		// 2. Instantiate Emscripten imports (IMPORTANT to provide all Emscripten environment functions including _emscripten_fs_load_embedded_files)
		_, err := emscripten.Instantiate(ctx, r)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to instantiate emscripten imports: %w", err)
		}

		// 3. Compile the WASM module after imports are ready
		module, err := r.CompileModule(ctx, binary)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to compile WASM module: %w", err)
		}
		compiledModule = module

		// No need to manually build "env" or stub syscalls here, emscripten.Instantiate handles that

		// Now ready to instantiate actual guest module using this runtime
	}

	return compiledModule, r, nil
}