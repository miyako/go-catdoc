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

		// 1. Instantiate WASI imports (needed)
		wasi_snapshot_preview1.MustInstantiate(ctx, r)

		// 2. Instantiate Emscripten imports (provides "env" module with required functions)
		_, err := emscripten.Instantiate(ctx, r)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to instantiate emscripten imports: %w", err)
		}

		// 3. Compile your WASM module AFTER emscripten imports are ready
		module, err := r.CompileModule(ctx, binary)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to compile WASM module: %w", err)
		}
		compiledModule = module
	}

	return compiledModule, r, nil
}

func getWASMModuleWithFS(file fs.FS, stdout, stderr io.Writer) (api.Module, error) {
	cMod, run, err := getCompiledWASMModule()
	if err != nil {
		return nil, err
	}

	srcCharset := os.Getenv("CATDOC_SRC_CHARSET")
	dstCharset := os.Getenv("CATDOC_DST_CHARSET")

	// Mount both the input file FS AND the embedded charsets directory to WASI FS
	modConfig := wazero.NewModuleConfig().
		WithStartFunctions("_initialize").
		WithEnv("CATDOC_SRC_CHARSET", srcCharset).
		WithEnv("CATDOC_DST_CHARSET", dstCharset).
		WithFSConfig(
			wazero.NewFSConfig().
				WithFSMount(file, "/input_file").
				WithFSMount(charsets, "/charsets"),
		).
		WithStdout(stdout).
		WithStderr(stderr)

	// Instantiate the compiled module using the runtime with Emscripten env module already instantiated
	mod, err := run.InstantiateModule(ctx, cMod, modConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate wasm module: %w", err)
	}

	return mod, nil
}
