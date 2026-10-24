@echo off
setlocal EnableExtensions EnableDelayedExpansion

set "ROOT_DIR=%~dp0"
if "%ROOT_DIR:~-1%"=="\" set "ROOT_DIR=%ROOT_DIR:~0,-1%"

set "WEB_DIR=%ROOT_DIR%\autocarto-web"
set "TAURI_DIR=%WEB_DIR%\src-tauri"
set "AGENT_DIR=%ROOT_DIR%\carto_agent"
set "PLUGIN_DIR=%ROOT_DIR%\autocarto"
set "DIST_DIR=%ROOT_DIR%\dist"
set "BINARIES_DIR=%TAURI_DIR%\binaries"
set "NODE_MODULES_DIR=%WEB_DIR%\node_modules"
set "TAURI_CMD=%NODE_MODULES_DIR%\.bin\tauri.cmd"
set "NEXT_CMD=%NODE_MODULES_DIR%\.bin\next.cmd"
set "DEPS_STAMP_FILE=%WEB_DIR%\.deps-lock.sha256"
set "QGIS_PLUGINS_DIR=%APPDATA%\QGIS\QGIS3\profiles\default\python\plugins"
set "DESKTOP_ARTIFACT_HINT=%TAURI_DIR%\target\release\bundle"
set "GOTOOLCHAIN=local"
set "GO_VERSION_MIN=unknown"
set "GO_TOOLCHAIN_REQUIRED="
set "GO_BUILD_CGO_ENABLED=0"
set "GO_BUILD_CC="
set "GO_BUILD_CXX="
set "GO_C_COMPILER_FOUND=0"
set "GO_C_COMPILER_NAME="
set "GO_CXX_COMPILER_NAME="
set "GO_C_COMPILER_PATH="
set "TAURI_CARGO_TARGET_DIR=F:\AutoCarto\tauri-target"
if not defined TAURI_CARGO_BUILD_JOBS set "TAURI_CARGO_BUILD_JOBS=2"

call :bootstrap_tool_paths
call :clear_native_toolchain_overrides

if /I "%PROCESSOR_ARCHITECTURE%"=="AMD64" (
  set "TARGET_TRIPLE=x86_64-pc-windows-msvc"
  set "GOARCH_TARGET=amd64"
) else if /I "%PROCESSOR_ARCHITECTURE%"=="ARM64" (
  set "TARGET_TRIPLE=aarch64-pc-windows-msvc"
  set "GOARCH_TARGET=arm64"
) else (
  call :fail "      : %PROCESSOR_ARCHITECTURE%"
  exit /b 1
)
set "GOOS_TARGET=windows"
set "SIDECAR_PATH=%BINARIES_DIR%\carto-agent-%TARGET_TRIPLE%.exe"

set "CMD=%~1"
if "%CMD%"=="" set "CMD=build"

if /I "%CMD%"=="build" goto :cmd_build
if /I "%CMD%"=="dev" goto :cmd_dev
if /I "%CMD%"=="deps" goto :cmd_deps
if /I "%CMD%"=="go" goto :cmd_go
if /I "%CMD%"=="check" goto :cmd_check
if /I "%CMD%"=="clean" goto :cmd_clean
if /I "%CMD%"=="run" goto :cmd_run
if /I "%CMD%"=="help" goto :cmd_help
if /I "%CMD%"=="app" (
  call :fail "app     macOS    Windows     build"
  exit /b 1
)
if /I "%CMD%"=="dmg" (
  call :fail "dmg     macOS"
  exit /b 1
)

call :warn "    : %CMD%"
call :cmd_help
exit /b 1

:cmd_build
call :preflight build || exit /b 1
call :clean_local_artifacts || exit /b 1
call :build_go || exit /b 1
call :build_desktop || exit /b 1
call :deploy_plugin || exit /b 1
call :step     
call :ok "         %DESKTOP_ARTIFACT_HINT%"
call :ok "Agent sidecar %SIDECAR_PATH%"
call :ok "QGIS      %QGIS_PLUGINS_DIR%"
exit /b 0

:cmd_dev
call :preflight dev || exit /b 1
call :clean_local_artifacts || exit /b 1
call :build_go || exit /b 1
call :deploy_plugin || exit /b 1
call :run_tauri_dev || exit /b 1
exit /b 0

:cmd_deps
call :preflight deps || exit /b 1
call :run_deps || exit /b 1
exit /b 0

:cmd_go
call :preflight go || exit /b 1
call :clean_local_artifacts || exit /b 1
call :build_go || exit /b 1
exit /b 0

:cmd_run
call :preflight go || exit /b 1
call :clean_local_artifacts || exit /b 1
call :build_go || exit /b 1
if not defined AUTOCARTO_HOME set "AUTOCARTO_HOME=%ROOT_DIR%\autocarto_data"
set "AUTOCARTO_ASSETS_ROOT=%PLUGIN_DIR%"
"%SIDECAR_PATH%"
exit /b 0

:cmd_check
call :preflight check || exit /b 1
call :clean_local_artifacts || exit /b 1
call :run_check || exit /b 1
exit /b 0

:cmd_clean
call :clean
exit /b 0

:cmd_help
echo AutoCarto AI Windows     
echo.
echo   : build.bat [  ]
echo.
echo   :
echo   build           + Go sidecar + Tauri      + QGIS         
echo   dev        + sidecar + QGIS      + Tauri     
echo   deps           npm ci 
echo   go          Go sidecar
echo   check        +      +     
echo   clean         
echo   help        
echo.
echo   :
echo   -                                 
echo   -              build.bat deps
echo   - build/dev       QGIS                   QGIS 
exit /b 0

:step
echo.
echo === %~1 ===
exit /b 0

:info
echo [INFO] %~1
exit /b 0

:ok
echo [OK] %~1
exit /b 0

:warn
echo [WARN] %~1
exit /b 0

:fail
echo [FAIL] %~1
exit /b 1

:mark_preflight_failure
call :warn "%~1"
set "PRECHECK_FAILED=1"
exit /b 0

:preflight
set "PRECHECK_FAILED=0"
set "CURRENT_COMMAND=%~1"
call :step "     %CURRENT_COMMAND% "

call :check_repo_layout "%CURRENT_COMMAND%"
call :load_go_requirements
call :configure_go_cgo "%CURRENT_COMMAND%"

if /I "%CURRENT_COMMAND%"=="go" (
  call :check_go_toolchain
) else if /I "%CURRENT_COMMAND%"=="check" (
  call :check_go_toolchain
  call :check_node_toolchain
  where rustc >nul 2>&1
  if errorlevel 1 call :mark_preflight_failure "   rustc      Rust        https://www.rust-lang.org/tools/install"
  where cargo >nul 2>&1
  if errorlevel 1 call :mark_preflight_failure "   cargo      Rust        https://www.rust-lang.org/tools/install"
  call :check_frontend_check_deps
  call :check_msvc_tools
) else if /I "%CURRENT_COMMAND%"=="build" (
  call :check_go_toolchain
  call :check_node_toolchain
  where rustc >nul 2>&1
  if errorlevel 1 call :mark_preflight_failure "   rustc      Rust        https://www.rust-lang.org/tools/install"
  where cargo >nul 2>&1
  if errorlevel 1 call :mark_preflight_failure "   cargo      Rust        https://www.rust-lang.org/tools/install"
  call :check_frontend_deps
  call :check_msvc_tools
  call :check_qgis_plugins_dir
) else if /I "%CURRENT_COMMAND%"=="dev" (
  call :check_go_toolchain
  call :check_node_toolchain
  where rustc >nul 2>&1
  if errorlevel 1 call :mark_preflight_failure "   rustc      Rust        https://www.rust-lang.org/tools/install"
  where cargo >nul 2>&1
  if errorlevel 1 call :mark_preflight_failure "   cargo      Rust        https://www.rust-lang.org/tools/install"
  call :check_frontend_deps
  call :check_msvc_tools
  call :check_qgis_plugins_dir
) else if /I "%CURRENT_COMMAND%"=="deps" (
  call :check_node_toolchain
)

if "%PRECHECK_FAILED%"=="1" (
  call :fail "             "
  exit /b 1
)
call :ok "      "
exit /b 0

:check_repo_layout
set "LAYOUT_COMMAND=%~1"
for %%C in (build dev check) do if /I "%LAYOUT_COMMAND%"=="%%C" (
  if not exist "%PLUGIN_DIR%\flows\system" call :mark_preflight_failure "缺少内置流程目录，无法完整打包运行资产"
  if not exist "%PLUGIN_DIR%\resources\system" call :mark_preflight_failure "缺少内置资源库目录，无法完整打包运行资产"
)
if /I "%LAYOUT_COMMAND%"=="go" (
  if not exist "%AGENT_DIR%" call :mark_preflight_failure "   Go           sidecar %AGENT_DIR% "
  if not exist "%TAURI_DIR%" call :mark_preflight_failure "   Tauri         sidecar    %TAURI_DIR% "
  if not exist "%AGENT_DIR%\go.mod" call :mark_preflight_failure "   carto_agent\go.mod      Go toolchain   "
  exit /b 0
)
if /I "%LAYOUT_COMMAND%"=="check" (
  if not exist "%WEB_DIR%" call :mark_preflight_failure "            Next.js/Tauri      %WEB_DIR% "
  if not exist "%TAURI_DIR%" call :mark_preflight_failure "   Tauri              %TAURI_DIR% "
  if not exist "%AGENT_DIR%" call :mark_preflight_failure "   Go           Go    %AGENT_DIR% "
  if not exist "%WEB_DIR%\package-lock.json" call :mark_preflight_failure "   package-lock.json           "
  if not exist "%AGENT_DIR%\go.mod" call :mark_preflight_failure "   carto_agent\go.mod      Go toolchain   "
  if not exist "%TAURI_DIR%\Cargo.toml" call :mark_preflight_failure "   Cargo.toml      Tauri     "
  exit /b 0
)
if /I "%LAYOUT_COMMAND%"=="build" (
  if not exist "%WEB_DIR%" call :mark_preflight_failure "            Next.js/Tauri    %WEB_DIR% "
  if not exist "%TAURI_DIR%" call :mark_preflight_failure "   Tauri             %TAURI_DIR% "
  if not exist "%AGENT_DIR%" call :mark_preflight_failure "   Go           sidecar %AGENT_DIR% "
  if not exist "%PLUGIN_DIR%" call :mark_preflight_failure "   autocarto             %PLUGIN_DIR% "
  if not exist "%WEB_DIR%\package-lock.json" call :mark_preflight_failure "   package-lock.json           "
  if not exist "%AGENT_DIR%\go.mod" call :mark_preflight_failure "   carto_agent\go.mod      Go toolchain   "
  if not exist "%TAURI_DIR%\Cargo.toml" call :mark_preflight_failure "   Cargo.toml      Tauri   "
  if not exist "%TAURI_DIR%\tauri.conf.json" call :mark_preflight_failure "   tauri.conf.json      sidecar     "
)
if /I "%LAYOUT_COMMAND%"=="dev" (
  if not exist "%WEB_DIR%" call :mark_preflight_failure "            Next.js/Tauri    %WEB_DIR% "
  if not exist "%TAURI_DIR%" call :mark_preflight_failure "   Tauri             %TAURI_DIR% "
  if not exist "%AGENT_DIR%" call :mark_preflight_failure "   Go           sidecar %AGENT_DIR% "
  if not exist "%PLUGIN_DIR%" call :mark_preflight_failure "   autocarto             %PLUGIN_DIR% "
  if not exist "%WEB_DIR%\package-lock.json" call :mark_preflight_failure "   package-lock.json           "
  if not exist "%AGENT_DIR%\go.mod" call :mark_preflight_failure "   carto_agent\go.mod      Go toolchain   "
  if not exist "%TAURI_DIR%\Cargo.toml" call :mark_preflight_failure "   Cargo.toml      Tauri   "
  if not exist "%TAURI_DIR%\tauri.conf.json" call :mark_preflight_failure "   tauri.conf.json      sidecar     "
)
if /I "%LAYOUT_COMMAND%"=="deps" (
  if not exist "%WEB_DIR%" call :mark_preflight_failure "              %WEB_DIR% "
  if not exist "%WEB_DIR%\package-lock.json" call :mark_preflight_failure "   package-lock.json           "
)
exit /b 0

:check_go_toolchain
where go >nul 2>&1
if errorlevel 1 (
  if /I "%GO_VERSION_MIN%"=="unknown" (
    call :mark_preflight_failure "   Go          Go        https://go.dev/dl/"
  ) else if defined GO_TOOLCHAIN_REQUIRED (
    call :mark_preflight_failure "   Go     Go ^>= %GO_VERSION_MIN%    %GO_TOOLCHAIN_REQUIRED%     https://go.dev/dl/"
  ) else (
    call :mark_preflight_failure "   Go     Go ^>= %GO_VERSION_MIN%     https://go.dev/dl/"
  )
  exit /b 0
)

for /f "tokens=3" %%A in ('go version') do set "INSTALLED_GO_VERSION=%%A"
set "INSTALLED_GO_VERSION=%INSTALLED_GO_VERSION:go=%"

if /I not "%GO_VERSION_MIN%"=="unknown" (
  if defined GO_TOOLCHAIN_REQUIRED (
    powershell -NoProfile -Command "if (([version]'%INSTALLED_GO_VERSION%' -ge [version]'%GO_VERSION_MIN%') -and ([version]'%INSTALLED_GO_VERSION%' -ge [version]'%GO_TOOLCHAIN_REQUIRED%')) { exit 0 } else { exit 1 }" >nul
    if errorlevel 1 call :mark_preflight_failure "   Go     %INSTALLED_GO_VERSION%        go %GO_VERSION_MIN% toolchain %GO_TOOLCHAIN_REQUIRED%                        "
  ) else (
    powershell -NoProfile -Command "if ([version]'%INSTALLED_GO_VERSION%' -ge [version]'%GO_VERSION_MIN%') { exit 0 } else { exit 1 }" >nul
    if errorlevel 1 call :mark_preflight_failure "   Go     %INSTALLED_GO_VERSION%        go %GO_VERSION_MIN%      Go     "
  )
)
exit /b 0

:load_go_requirements
set "GO_VERSION_MIN=unknown"
set "GO_TOOLCHAIN_REQUIRED="
if not exist "%AGENT_DIR%\go.mod" exit /b 0
for /f "tokens=2" %%A in ('findstr /b /c:"go " "%AGENT_DIR%\go.mod"') do set "GO_VERSION_MIN=%%A"
for /f "tokens=2" %%A in ('findstr /b /c:"toolchain " "%AGENT_DIR%\go.mod"') do set "GO_TOOLCHAIN_REQUIRED=%%A"
set "GO_TOOLCHAIN_REQUIRED=%GO_TOOLCHAIN_REQUIRED:go=%"
exit /b 0

:check_node_toolchain
where node >nul 2>&1
if errorlevel 1 call :mark_preflight_failure "   Node.js      Node.js     https://nodejs.org/"
where npm >nul 2>&1
if errorlevel 1 call :mark_preflight_failure "   npm     Node.js         "
exit /b 0

:check_rust_toolchain
where rustc >nul 2>&1
if errorlevel 1 call :mark_preflight_failure "   rustc      Rust        https://www.rust-lang.org/tools/install"
where cargo >nul 2>&1
if errorlevel 1 call :mark_preflight_failure "   cargo      Rust        https://www.rust-lang.org/tools/install"
exit /b 0

:check_frontend_deps
if not exist "%NODE_MODULES_DIR%" call :mark_preflight_failure "   autocarto-web\node_modules      build.bat deps"
if not exist "%TAURI_CMD%" call :mark_preflight_failure "     Tauri CLI      build.bat deps"
if not exist "%NEXT_CMD%" call :mark_preflight_failure "     Next CLI      build.bat deps"
exit /b 0

:check_frontend_check_deps
if not exist "%NODE_MODULES_DIR%" call :mark_preflight_failure "   autocarto-web\node_modules      build.bat deps"
if not exist "%NEXT_CMD%" call :mark_preflight_failure "     Next CLI      build.bat deps"
exit /b 0

:check_msvc_tools
call :ensure_msvc_env
where cl >nul 2>&1
if errorlevel 1 call :mark_preflight_failure "     Microsoft C++ Build Tools cl.exe      Visual Studio Build Tools     Desktop development with C++         Developer Command Prompt       "
exit /b 0

:ensure_msvc_env
where cl >nul 2>&1
if not errorlevel 1 exit /b 0

for %%P in (
  "%ProgramFiles%\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat"
  "%ProgramFiles(x86)%\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat"
  "D:\VisualStudio\VC\Auxiliary\Build\vcvars64.bat"
) do (
  if exist "%%~P" (
    call "%%~P" >nul 2>&1
    where cl >nul 2>&1
    if not errorlevel 1 (
      call :info "MSVC environment loaded from %%~P"
      exit /b 0
    )
  )
)
exit /b 0

:check_qgis_plugins_dir
if not exist "%QGIS_PLUGINS_DIR%" call :mark_preflight_failure "    QGIS      %QGIS_PLUGINS_DIR%           QGIS    profile     build.bat go / build.bat check          "
exit /b 0

:build_go
call :step "   Go sidecar"
if not exist "%BINARIES_DIR%" mkdir "%BINARIES_DIR%"
pushd "%AGENT_DIR%"
set "CGO_ENABLED=%GO_BUILD_CGO_ENABLED%"
if defined GO_BUILD_CC (
  set "CC=%GO_BUILD_CC%"
) else (
  set "CC="
)
if defined GO_BUILD_CXX (
  set "CXX=%GO_BUILD_CXX%"
) else (
  set "CXX="
)
call :info "Go env: CGO_ENABLED=%CGO_ENABLED% CC=%CC% CXX=%CXX%"
go build -o "%SIDECAR_PATH%" .\cmd\agent
if errorlevel 1 (
  popd
  call :fail "Agent sidecar     "
  exit /b 1
)
popd

if not exist "%SIDECAR_PATH%" (
  call :fail "sidecar            %SIDECAR_PATH%"
  exit /b 1
)
call :ok "sidecar     %SIDECAR_PATH%"
exit /b 0

:build_desktop
call :step "      "
call :clear_native_toolchain_overrides
set "CARGO_TARGET_DIR=%TAURI_CARGO_TARGET_DIR%"
set "CARGO_BUILD_JOBS=%TAURI_CARGO_BUILD_JOBS%"
call :info "Rust env: CARGO_TARGET_DIR=%CARGO_TARGET_DIR% CARGO_BUILD_JOBS=%CARGO_BUILD_JOBS%"
pushd "%WEB_DIR%"
call "%TAURI_CMD%" build
if errorlevel 1 (
  popd
  call :fail "Tauri         "
  exit /b 1
)
popd
call :ok "         %DESKTOP_ARTIFACT_HINT%"
exit /b 0

:run_check
call :step "Go     "
pushd "%AGENT_DIR%"
set "CGO_ENABLED=%GO_BUILD_CGO_ENABLED%"
if defined GO_BUILD_CC (
  set "CC=%GO_BUILD_CC%"
) else (
  set "CC="
)
if defined GO_BUILD_CXX (
  set "CXX=%GO_BUILD_CXX%"
) else (
  set "CXX="
)
call :info "Go env: CGO_ENABLED=%CGO_ENABLED% CC=%CC% CXX=%CXX%"
go build ./...
if errorlevel 1 (
  popd
  call :fail "carto_agent     "
  exit /b 1
)
call :ok "carto_agent     "

call :step "Go     "
go vet ./...
if errorlevel 1 (
  popd
  call :fail "carto_agent vet   "
  exit /b 1
)
call :ok "carto_agent vet   "

call :step "Go     "
go test ./...
if errorlevel 1 (
  popd
  call :fail "carto_agent     "
  exit /b 1
)
popd
call :ok "carto_agent     "

call :clear_native_toolchain_overrides
call :step "   Lint   "
pushd "%WEB_DIR%"
call npm run lint
if errorlevel 1 (
  popd
  call :fail "   Lint   "
  exit /b 1
)
popd
call :ok "   Lint   "

call :step "      "
pushd "%WEB_DIR%"
call npm test
if errorlevel 1 (
  popd
  call :fail "        "
  exit /b 1
)
popd
call :ok "        "

call :step "Python   "
pushd "%ROOT_DIR%"
python -m pytest autocarto/tests/ -v
if errorlevel 1 (
  popd
  call :fail "Python     "
  exit /b 1
)
popd
call :ok "Python     "

call :step "Next.js     "
pushd "%WEB_DIR%"
call "%NEXT_CMD%" build
if errorlevel 1 (
  popd
  call :fail "      "
  exit /b 1
)
popd
call :ok "      "

call :step "Rust     "
set "CARGO_TARGET_DIR=%TAURI_CARGO_TARGET_DIR%"
set "CARGO_BUILD_JOBS=%TAURI_CARGO_BUILD_JOBS%"
call :info "Rust env: CARGO_TARGET_DIR=%CARGO_TARGET_DIR% CARGO_BUILD_JOBS=%CARGO_BUILD_JOBS%"
cargo check --release --manifest-path "%TAURI_DIR%\Cargo.toml"
if errorlevel 1 (
  call :fail "Tauri     "
  exit /b 1
)
call :ok "Tauri     "
call :ok "      "
exit /b 0

:deploy_plugin
call :step "   QGIS   "

if not exist "%QGIS_PLUGINS_DIR%\autocarto" mkdir "%QGIS_PLUGINS_DIR%\autocarto"

robocopy "%PLUGIN_DIR%" "%QGIS_PLUGINS_DIR%\autocarto" /MIR /R:1 /W:1 /XD "%PLUGIN_DIR%\data" logs .session __pycache__ /XF .master_key secrets.enc .port plugin_config.yaml *.db *.db-wal *.db-shm *.sqlite >nul
if errorlevel 8 (
  call :fail "   autocarto     "
  exit /b 1
)

call :ok "QGIS        %QGIS_PLUGINS_DIR%"
exit /b 0

:run_deps
call :step "      "
call :read_package_lock_hash
set "SHOULD_INSTALL=1"

if /I "%AUTOCARTO_FORCE_DEPS%"=="1" (
  call :info "AUTOCARTO_FORCE_DEPS=1, forcing npm ci"
) else (
  if exist "%NODE_MODULES_DIR%" if exist "%TAURI_CMD%" if exist "%NEXT_CMD%" if exist "%DEPS_STAMP_FILE%" (
    set "CACHED_LOCK_HASH="
    set /p "CACHED_LOCK_HASH="<"%DEPS_STAMP_FILE%"
    if /I "!CACHED_LOCK_HASH!"=="!CURRENT_LOCK_HASH!" (
      set "SHOULD_INSTALL=0"
    )
  )
)

if "%SHOULD_INSTALL%"=="0" (
  call :ok "Frontend deps unchanged; skipping npm ci"
  exit /b 0
)

pushd "%WEB_DIR%"
call npm ci
if errorlevel 1 (
  popd
  call :fail "        "
  exit /b 1
)
popd
if defined CURRENT_LOCK_HASH (
  >"%DEPS_STAMP_FILE%" echo !CURRENT_LOCK_HASH!
)
call :ok "Frontend dependencies installed"
exit /b 0

:read_package_lock_hash
set "CURRENT_LOCK_HASH="
set "LOCK_HASH_TMP=%TEMP%\autocarto-lock-hash.tmp"
powershell -NoProfile -Command "$hash = Get-FileHash -Algorithm SHA256 -LiteralPath '%WEB_DIR%\package-lock.json'; [Console]::Write($hash.Hash.ToLower())" > "%LOCK_HASH_TMP%" 2>nul
if exist "%LOCK_HASH_TMP%" (
  set /p "CURRENT_LOCK_HASH="<"%LOCK_HASH_TMP%"
  del /q "%LOCK_HASH_TMP%" >nul 2>&1
)
if not defined CURRENT_LOCK_HASH (
  call :warn "Cannot read package-lock hash; will run npm ci"
)
exit /b 0

:run_tauri_dev
call :step "      "
call :info "     http://localhost:3000"
call :info "  Ctrl+C   "
call :clear_native_toolchain_overrides
set "CARGO_TARGET_DIR=%TAURI_CARGO_TARGET_DIR%"
set "CARGO_BUILD_JOBS=%TAURI_CARGO_BUILD_JOBS%"
call :info "Rust env: CARGO_TARGET_DIR=%CARGO_TARGET_DIR% CARGO_BUILD_JOBS=%CARGO_BUILD_JOBS%"
pushd "%WEB_DIR%"
call "%TAURI_CMD%" dev
if errorlevel 1 (
  popd
  call :fail "Tauri         "
  exit /b 1
)
popd
exit /b 0

:clean_local_artifacts
call :step "      "
call :stop_stale_project_processes

if exist "%TAURI_DIR%\target" (
  rmdir /s /q "%TAURI_DIR%\target" >nul 2>&1
  if exist "%TAURI_DIR%\target" call :warn "   (      ): %TAURI_DIR%\target"
)

if exist "%TAURI_CARGO_TARGET_DIR%" (
  rmdir /s /q "%TAURI_CARGO_TARGET_DIR%" >nul 2>&1
  if exist "%TAURI_CARGO_TARGET_DIR%" (
    set "TAURI_CARGO_TARGET_DIR=F:\AutoCarto\tauri-target-%RANDOM%%RANDOM%"
    call :warn "   Tauri target       ,     : %TAURI_CARGO_TARGET_DIR%"
  )
)

if exist "%WEB_DIR%\out" (
  rmdir /s /q "%WEB_DIR%\out" >nul 2>&1
  if exist "%WEB_DIR%\out" call :warn "   (      ): %WEB_DIR%\out"
)

if exist "%WEB_DIR%\.next" (
  rmdir /s /q "%WEB_DIR%\.next" >nul 2>&1
  if exist "%WEB_DIR%\.next" call :warn "   (      ): %WEB_DIR%\.next"
)

if exist "%DIST_DIR%" (
  rmdir /s /q "%DIST_DIR%" >nul 2>&1
  if exist "%DIST_DIR%" call :warn "   (      ): %DIST_DIR%"
)

if exist "%BINARIES_DIR%" (
  rmdir /s /q "%BINARIES_DIR%" >nul 2>&1
  if exist "%BINARIES_DIR%" call :warn "   (      ): %BINARIES_DIR%"
)

if exist "%PLUGIN_DIR%\bin" (
  rmdir /s /q "%PLUGIN_DIR%\bin" >nul 2>&1
  if exist "%PLUGIN_DIR%\bin" call :warn "   (      ): %PLUGIN_DIR%\bin"
)

if exist "%PLUGIN_DIR%\logs" (
  rmdir /s /q "%PLUGIN_DIR%\logs" >nul 2>&1
  if exist "%PLUGIN_DIR%\logs" call :warn "   (      ): %PLUGIN_DIR%\logs"
)

if exist "%PLUGIN_DIR%\.session" (
  rmdir /s /q "%PLUGIN_DIR%\.session" >nul 2>&1
  if exist "%PLUGIN_DIR%\.session" call :warn "   (      ): %PLUGIN_DIR%\.session"
)

call :ok "    "
exit /b 0

:clean
call :clean_local_artifacts
exit /b 0

:stop_stale_project_processes
set "KILLED_COUNT=0"
for %%P in (autocarto-ai.exe carto-agent.exe cargo.exe rustc.exe) do (
  taskkill /f /im %%P >nul 2>&1
  if not errorlevel 1 set /a KILLED_COUNT+=1
)
if not "%KILLED_COUNT%"=="0" call :info "      %KILLED_COUNT%              "
set "KILLED_COUNT="
exit /b 0

:bootstrap_tool_paths
for %%P in (
  "D:\Go\bin"
  "%ProgramFiles%\Go\bin"
  "D:\software\nodejs"
  "%ProgramFiles%\nodejs"
  "%LOCALAPPDATA%\Programs\nodejs"
  "%USERPROFILE%\.cargo\bin"
  "C:\w64devkit\bin"
  "%USERPROFILE%\w64devkit\bin"
  "C:\msys64\ucrt64\bin"
  "C:\msys64\mingw64\bin"
  "%ProgramFiles%\LLVM\bin"
  "%USERPROFILE%\miniconda3\Library\bin"
  "%USERPROFILE%\anaconda3\Library\bin"
) do (
  if exist "%%~P" (
    set "PATH_CHECK=!PATH:%%~P=!"
    if "!PATH_CHECK!"=="!PATH!" set "PATH=%%~P;!PATH!"
  )
)
if defined CONDA_PREFIX (
  if exist "%CONDA_PREFIX%\Library\bin" (
    set "PATH_CHECK=!PATH:%CONDA_PREFIX%\Library\bin=!"
    if "!PATH_CHECK!"=="!PATH!" set "PATH=%CONDA_PREFIX%\Library\bin;!PATH!"
  )
)
exit /b 0

:configure_go_cgo
set "CGO_COMMAND=%~1"
set "GO_BUILD_CGO_ENABLED=0"
set "GO_BUILD_CC="
set "GO_BUILD_CXX="
set "GO_C_COMPILER_FOUND=0"
set "GO_C_COMPILER_NAME="
set "GO_CXX_COMPILER_NAME="
set "GO_C_COMPILER_PATH="

if /I "%CGO_COMMAND%"=="deps" exit /b 0
if /I "%AUTOCARTO_CGO_MODE%"=="off" (
  call :warn "AUTOCARTO_CGO_MODE=off, force CGO_ENABLED=0"
  exit /b 0
)

call :detect_cgo_compiler
if "%GO_C_COMPILER_FOUND%"=="1" (
  set "GO_BUILD_CGO_ENABLED=1"
  set "GO_BUILD_CC=%GO_C_COMPILER_NAME%"
  if defined GO_CXX_COMPILER_NAME (
    set "GO_BUILD_CXX=%GO_CXX_COMPILER_NAME%"
  ) else if /I "%GO_C_COMPILER_NAME%"=="gcc" (
    set "GO_BUILD_CXX=g++"
  ) else if /I "%GO_C_COMPILER_NAME%"=="clang" (
    set "GO_BUILD_CXX=clang++"
  )
  call :info "Go CGO compiler: %GO_C_COMPILER_NAME% (%GO_C_COMPILER_PATH%)"
  exit /b 0
)

if /I "%AUTOCARTO_ALLOW_CGO_FALLBACK%"=="1" (
  call :warn "No C compiler found for CGO, AUTOCARTO_ALLOW_CGO_FALLBACK=1 so fallback to CGO_ENABLED=0."
  exit /b 0
)

if /I "%CGO_COMMAND%"=="build" (
  call :mark_preflight_failure "No C compiler found for CGO. build needs CGO (go-sqlite3). Install gcc (MSYS2/MinGW) or clang."
  exit /b 0
)
if /I "%CGO_COMMAND%"=="dev" (
  call :mark_preflight_failure "No C compiler found for CGO. dev needs CGO (go-sqlite3). Install gcc (MSYS2/MinGW) or clang."
  exit /b 0
)
if /I "%CGO_COMMAND%"=="go" (
  call :mark_preflight_failure "No C compiler found for CGO. sidecar build needs CGO (go-sqlite3). Install gcc (MSYS2/MinGW) or clang."
  exit /b 0
)
if /I "%CGO_COMMAND%"=="check" (
  call :mark_preflight_failure "No C compiler found for CGO. check needs CGO (go-sqlite3). Install gcc (MSYS2/MinGW) or clang."
  exit /b 0
)

call :warn "No C compiler found for CGO, fallback to CGO_ENABLED=0 for %CGO_COMMAND%."
exit /b 0

:detect_cgo_compiler
set "GO_C_COMPILER_FOUND=0"
set "GO_C_COMPILER_NAME="
set "GO_CXX_COMPILER_NAME="
set "GO_C_COMPILER_PATH="
call :try_cgo_compiler "x86_64-w64-mingw32-gcc" "x86_64-w64-mingw32-g++"
if "%GO_C_COMPILER_FOUND%"=="1" exit /b 0
call :try_cgo_compiler "gcc" "g++"
if "%GO_C_COMPILER_FOUND%"=="1" exit /b 0
call :try_cgo_compiler "clang" "clang++"
if "%GO_C_COMPILER_FOUND%"=="1" exit /b 0
exit /b 0

:try_cgo_compiler
if "%GO_C_COMPILER_FOUND%"=="1" exit /b 0
set "CGO_TRY_CC=%~1"
set "CGO_TRY_CXX=%~2"
if "%CGO_TRY_CC%"=="" exit /b 0

where "%CGO_TRY_CC%" >nul 2>&1
if errorlevel 1 exit /b 0

for /f "delims=" %%P in ('where "%CGO_TRY_CC%"') do (
  if "!GO_C_COMPILER_FOUND!"=="0" (
    call :validate_cgo_compiler "%%P"
    if "!CGO_COMPILER_USABLE!"=="1" (
      set "GO_C_COMPILER_FOUND=1"
      set "GO_C_COMPILER_NAME=%%P"
      set "GO_C_COMPILER_PATH=%%P"
      if defined CGO_TRY_CXX (
        if exist "%%~dpP%CGO_TRY_CXX%.exe" (
          set "GO_CXX_COMPILER_NAME=%%~dpP%CGO_TRY_CXX%.exe"
        ) else (
          set "GO_CXX_COMPILER_NAME=%CGO_TRY_CXX%"
        )
      ) else (
        set "GO_CXX_COMPILER_NAME="
      )
    ) else (
      if defined CGO_COMPILER_REASON (
        call :warn "Skip C compiler %%P: !CGO_COMPILER_REASON!"
      ) else (
        call :warn "Skip C compiler %%P: probe failed."
      )
    )
  )
)

if "%GO_C_COMPILER_FOUND%"=="0" (
  if defined CGO_COMPILER_REASON (
    call :warn "Skip C compiler %CGO_TRY_CC%: %CGO_COMPILER_REASON%"
  ) else (
    call :warn "Skip C compiler %CGO_TRY_CC%: not usable."
  )
  exit /b 0
)

exit /b 0

:validate_cgo_compiler
set "CGO_COMPILER_USABLE=0"
set "CGO_COMPILER_REASON="
set "CGO_VALIDATE_CC=%~1"
if "%CGO_VALIDATE_CC%"=="" (
  set "CGO_COMPILER_REASON=missing compiler name"
  exit /b 0
)

set "CGO_PROBE_ROOT=%TEMP%\autocarto_cgo_probe_%RANDOM%%RANDOM%"
set "CGO_PROBE_C=%CGO_PROBE_ROOT%.c"
set "CGO_PROBE_O=%CGO_PROBE_ROOT%.o"

> "%CGO_PROBE_C%" (
  echo int autocarto_cgo_probe = 0;
)

"%CGO_VALIDATE_CC%" -c "%CGO_PROBE_C%" -o "%CGO_PROBE_O%" >nul 2>&1
if errorlevel 1 (
  set "CGO_COMPILER_REASON=cannot compile probe source"
  if exist "%CGO_PROBE_C%" del /f /q "%CGO_PROBE_C%" >nul 2>&1
  if exist "%CGO_PROBE_O%" del /f /q "%CGO_PROBE_O%" >nul 2>&1
  exit /b 0
)

set "CGO_PROBE_MAGIC="
for /f %%H in ('powershell -NoProfile -Command "$b=[IO.File]::ReadAllBytes('%CGO_PROBE_O%'); if($b.Length -lt 4){'SHORT'} else {'{0:X2}{1:X2}{2:X2}{3:X2}' -f $b[0],$b[1],$b[2],$b[3]}"') do set "CGO_PROBE_MAGIC=%%H"

if /I "%CGO_PROBE_MAGIC%"=="SHORT" (
  set "CGO_COMPILER_REASON=probe object too small"
  if exist "%CGO_PROBE_C%" del /f /q "%CGO_PROBE_C%" >nul 2>&1
  if exist "%CGO_PROBE_O%" del /f /q "%CGO_PROBE_O%" >nul 2>&1
  exit /b 0
)

if /I "%CGO_PROBE_MAGIC%"=="0000FFFF" (
  set "CGO_COMPILER_REASON=emits COFF bigobj, current Go cgo parser rejects it"
  if exist "%CGO_PROBE_C%" del /f /q "%CGO_PROBE_C%" >nul 2>&1
  if exist "%CGO_PROBE_O%" del /f /q "%CGO_PROBE_O%" >nul 2>&1
  exit /b 0
)

set "CGO_COMPILER_USABLE=1"
if exist "%CGO_PROBE_C%" del /f /q "%CGO_PROBE_C%" >nul 2>&1
if exist "%CGO_PROBE_O%" del /f /q "%CGO_PROBE_O%" >nul 2>&1
exit /b 0

:clear_native_toolchain_overrides
set "CGO_ENABLED="
set "CC="
set "CXX="
set "AR="
set "CFLAGS="
set "CXXFLAGS="
set "CPPFLAGS="
set "LDFLAGS="
set "PKG_CONFIG="
set "PKG_CONFIG_PATH="
set "PKG_CONFIG_LIBDIR="
exit /b 0

:apply_go_cgo_env
set "CGO_ENABLED=%GO_BUILD_CGO_ENABLED%"
if defined GO_BUILD_CC (
  set "CC=%GO_BUILD_CC%"
) else (
  set "CC="
)
if defined GO_BUILD_CXX (
  set "CXX=%GO_BUILD_CXX%"
) else (
  set "CXX="
)
call :info "Go env: CGO_ENABLED=%CGO_ENABLED% CC=%CC% CXX=%CXX%"
exit /b 0
