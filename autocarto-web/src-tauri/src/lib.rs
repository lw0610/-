use std::error::Error;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::Mutex;
use std::thread;
#[cfg(not(target_os = "macos"))]
use std::time::{Duration, Instant};
#[cfg(target_os = "macos")]
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tauri::Manager;
use tauri_plugin_dialog::{DialogExt, FilePath};
use tauri_plugin_shell::process::CommandChild;
use tauri_plugin_shell::ShellExt;
mod local_output;

struct ManagedSidecar {
    child: CommandChild,
    runtime: BackendRuntime,
    runtime_port_file: PathBuf,
}

struct SidecarState(Mutex<Option<ManagedSidecar>>);

struct StartupState(Mutex<Option<String>>);

#[derive(Clone, serde::Deserialize, serde::Serialize)]
struct BackendRuntime {
    port: u16,
    backend_url: String,
    ws_url: String,
    startup_error: Option<String>,
}

const SIDECAR_READY_TIMEOUT: Duration = Duration::from_secs(20);
const PLUGIN_ROOT_REQUIRED_MARKER: &str = "skills/system";
const PLUGIN_ROOT_SEARCH_DEPTH: usize = 8;
const RUNTIME_HOME_ENV_VAR: &str = "AUTOCARTO_HOME";
const ASSETS_ROOT_ENV_VAR: &str = "AUTOCARTO_ASSETS_ROOT";
const DESKTOP_PARENT_PID_ENV_VAR: &str = "AUTOCARTO_DESKTOP_PARENT_PID";
const PROJECT_ROOT_ENV_VAR: &str = "AUTOCARTO_PROJECT_ROOT";
const DEFAULT_RUNTIME_DIR: &str = "autocarto_data";
#[cfg(target_os = "windows")]
const WEBVIEW2_USER_DATA_ENV_VAR: &str = "WEBVIEW2_USER_DATA_FOLDER";

#[derive(Debug)]
struct PreparedSidecarLaunch {
    runtime_home: PathBuf,
    assets_root: PathBuf,
    runtime_port_file: PathBuf,
}

fn build_error(message: impl Into<String>) -> Box<dyn Error> {
    Box::new(std::io::Error::new(
        std::io::ErrorKind::Other,
        message.into(),
    ))
}

fn configure_default_runtime_environment() {
    // Share the same anchor with the Go sidecar and QGIS child processes.
    if let Ok(root) = resolve_project_root() {
        std::env::set_var(PROJECT_ROOT_ENV_VAR, root);
    }
    #[cfg(target_os = "windows")]
    if std::env::var_os(WEBVIEW2_USER_DATA_ENV_VAR).is_none() {
        if let Ok(home) =
            resolve_runtime_home_from_env(std::env::var(RUNTIME_HOME_ENV_VAR).ok().as_deref())
        {
            std::env::set_var(WEBVIEW2_USER_DATA_ENV_VAR, home.join("webview2"));
        }
    }
}

fn resolve_project_root() -> Result<PathBuf, String> {
    if let Ok(raw) = std::env::var(PROJECT_ROOT_ENV_VAR) {
        if !raw.trim().is_empty() {
            let path = expand_env_path(&raw)?;
            if !path.is_absolute() {
                return Err(format!("{PROJECT_ROOT_ENV_VAR} 必须为绝对路径"));
            }
            return Ok(path);
        }
    }
    if let Ok(raw) = std::env::var(ASSETS_ROOT_ENV_VAR) {
        let path = expand_env_path(&raw)?;
        if path.is_absolute() && is_plugin_root(&path) {
            if let Some(parent) = path.parent() {
                return Ok(parent.to_path_buf());
            }
        }
    }
    let executable = std::env::current_exe().ok();
    if let Some(path) = executable.as_ref() {
        // A portable macOS application keeps writable data beside its .app.
        if let Some(bundle) = path
            .ancestors()
            .find(|p| p.extension().map(|e| e == "app").unwrap_or(false))
        {
            if let Some(parent) = bundle.parent() {
                return Ok(parent.to_path_buf());
            }
        }
        if let Some(root) = path.parent().and_then(discover_plugin_root_from_seed) {
            return Ok(root.parent().unwrap_or(&root).to_path_buf());
        }
    }
    let source = Path::new(env!("CARGO_MANIFEST_DIR"));
    if let Some(root) = discover_plugin_root_from_seed(source) {
        return Ok(root.parent().unwrap_or(&root).to_path_buf());
    }
    Err(format!("未发现项目根目录，请设置 {PROJECT_ROOT_ENV_VAR}"))
}

fn resolve_project_path(raw: &str) -> Result<PathBuf, String> {
    let path = expand_env_path(raw)?;
    if path.is_absolute() {
        return Ok(path);
    }
    Ok(resolve_project_root()?.join(path))
}

fn is_plugin_root(path: &Path) -> bool {
    path.join("skills").join("system").is_dir()
}

fn discover_plugin_root_from_seed(seed: &Path) -> Option<PathBuf> {
    let mut current = seed.to_path_buf();
    for _ in 0..=PLUGIN_ROOT_SEARCH_DEPTH {
        let autocarto_child = current.join("autocarto");
        if is_plugin_root(&autocarto_child) {
            return Some(autocarto_child);
        }
        if is_plugin_root(&current) {
            return Some(current);
        }
        if !current.pop() {
            break;
        }
    }
    None
}

fn expand_env_path(raw_home: &str) -> Result<PathBuf, String> {
    let trimmed = raw_home.trim();
    if trimmed == "~" {
        let home = std::env::var("HOME")
            .or_else(|_| std::env::var("USERPROFILE"))
            .map_err(|e| format!("解析用户目录失败: {e}"))?;
        return Ok(PathBuf::from(home));
    }
    if let Some(rest) = trimmed.strip_prefix("~/") {
        let home = std::env::var("HOME")
            .or_else(|_| std::env::var("USERPROFILE"))
            .map_err(|e| format!("解析用户目录失败: {e}"))?;
        return Ok(PathBuf::from(home).join(rest));
    }
    if let Some(rest) = trimmed.strip_prefix("~\\") {
        let home = std::env::var("HOME")
            .or_else(|_| std::env::var("USERPROFILE"))
            .map_err(|e| format!("解析用户目录失败: {e}"))?;
        return Ok(PathBuf::from(home).join(rest));
    }
    Ok(PathBuf::from(trimmed))
}

fn validate_runtime_home(path: &Path, source: &str) -> Result<PathBuf, String> {
    if path.as_os_str().is_empty() {
        return Err(format!(
            "{source} 为空，要求指向桌面 app-local runtime home"
        ));
    }
    Ok(path.to_path_buf())
}

fn validate_assets_root(path: &Path, source: &str) -> Result<PathBuf, String> {
    if is_plugin_root(path) {
        return Ok(path.to_path_buf());
    }
    Err(format!(
        "{source} 无效: {}，要求根目录包含 {PLUGIN_ROOT_REQUIRED_MARKER}",
        path.display()
    ))
}

fn resolve_runtime_home_from_env(env_home: Option<&str>) -> Result<PathBuf, String> {
    if let Some(raw_home) = env_home {
        let trimmed = raw_home.trim();
        if !trimmed.is_empty() {
            let expanded_home = resolve_project_path(trimmed)?;
            return validate_runtime_home(&expanded_home, "AUTOCARTO_HOME 指向的目录");
        }
    }

    Ok(resolve_project_root()?.join(DEFAULT_RUNTIME_DIR))
}

fn resolve_assets_root_from_sources(
    env_assets_root: Option<&str>,
    cwd_seed: Option<PathBuf>,
    exec_seed: Option<PathBuf>,
) -> Result<PathBuf, String> {
    if let Some(raw_assets_root) = env_assets_root {
        let trimmed = raw_assets_root.trim();
        if !trimmed.is_empty() {
            let expanded_assets_root = resolve_project_path(trimmed)?;
            return validate_assets_root(&expanded_assets_root, "AUTOCARTO_ASSETS_ROOT 指向的目录");
        }
    }

    for seed in [cwd_seed, exec_seed].into_iter().flatten() {
        if let Some(root) = discover_plugin_root_from_seed(&seed) {
            return validate_assets_root(&root, "自动发现的 AutoCarto 插件根目录");
        }
    }

    Err(format!(
		"未发现 AutoCarto 运行资产根目录：已检查 {ASSETS_ROOT_ENV_VAR}，并已从当前目录和可执行目录向上最多 {PLUGIN_ROOT_SEARCH_DEPTH} 层查找，要求根目录包含 {PLUGIN_ROOT_REQUIRED_MARKER}"
    ))
}

#[cfg(test)]
fn prepare_sidecar_launch_from_sources(
    env_home: Option<&str>,
    env_assets_root: Option<&str>,
    cwd_seed: Option<PathBuf>,
    exec_seed: Option<PathBuf>,
) -> Result<PreparedSidecarLaunch, String> {
    let runtime_home = resolve_runtime_home_from_env(env_home)?;
    let assets_root = resolve_assets_root_from_sources(env_assets_root, cwd_seed, exec_seed)?;
    let runtime_port_file = runtime_home.join("data").join(".port");
    Ok(PreparedSidecarLaunch {
        runtime_home,
        assets_root,
        runtime_port_file,
    })
}

#[cfg(test)]
fn resolve_autocarto_home() -> Result<PathBuf, String> {
    let env_home = std::env::var(RUNTIME_HOME_ENV_VAR).ok();
    resolve_runtime_home_from_env(env_home.as_deref())
}

fn resolve_assets_root() -> Result<PathBuf, String> {
    let env_assets_root = std::env::var(ASSETS_ROOT_ENV_VAR).ok();
    let cwd_seed = std::env::current_dir().ok();
    let exec_seed = std::env::current_exe()
        .ok()
        .and_then(|exec_path| exec_path.parent().map(|parent| parent.to_path_buf()));
    resolve_assets_root_from_sources(env_assets_root.as_deref(), cwd_seed, exec_seed)
}

#[cfg(target_os = "macos")]
fn resolve_plugin_data_dir() -> Result<PathBuf, String> {
    let env_home = std::env::var(RUNTIME_HOME_ENV_VAR).ok();
    Ok(resolve_runtime_home_from_env(env_home.as_deref())?.join("data"))
}

#[cfg(test)]
fn runtime_port_file_path() -> Result<PathBuf, String> {
    let runtime_home = resolve_autocarto_home()?;
    Ok(runtime_home.join("data").join(".port"))
}

fn resolve_bundled_assets_root(app: &tauri::AppHandle) -> Option<PathBuf> {
    let resource_dir = app.path().resource_dir().ok()?;
    for candidate in [resource_dir.join("autocarto"), resource_dir] {
        if is_plugin_root(&candidate) {
            return Some(candidate);
        }
    }
    None
}

fn resolve_runtime_home_for_app(_app: &tauri::AppHandle) -> Result<PathBuf, String> {
    let env_home = std::env::var(RUNTIME_HOME_ENV_VAR).ok();
    resolve_runtime_home_from_env(env_home.as_deref())
}

fn prepare_sidecar_launch(app: &tauri::AppHandle) -> Result<PreparedSidecarLaunch, String> {
    let runtime_home = resolve_runtime_home_for_app(app)?;
    let env_assets_root = std::env::var(ASSETS_ROOT_ENV_VAR).ok();
    let explicit_assets_root =
        resolve_assets_root_from_sources(env_assets_root.as_deref(), None, None);
    let workspace_assets_root = || resolve_assets_root();
    let bundled_assets_root = || {
        resolve_bundled_assets_root(app)
            .ok_or_else(|| "bundled AutoCarto assets root not found".to_string())
    };
    let assets_root = if let Ok(explicit_assets_root) = explicit_assets_root {
        explicit_assets_root
    } else if cfg!(debug_assertions) {
        workspace_assets_root().or_else(|_| bundled_assets_root())?
    } else {
        bundled_assets_root().or_else(|_| workspace_assets_root())?
    };
    let runtime_port_file = runtime_home.join("data").join(".port");
    Ok(PreparedSidecarLaunch {
        runtime_home,
        assets_root,
        runtime_port_file,
    })
}

fn clear_runtime_port_file(runtime_port_file: &Path) {
    match fs::remove_file(runtime_port_file) {
        Ok(()) => {}
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => eprintln!(
            "failed to clear stale runtime port file {}: {err}",
            runtime_port_file.display()
        ),
    }
}

fn remove_runtime_port_file(runtime_port_file: &Path, owned_port: u16) {
    // A previous desktop must not erase a newer backend's discovery file.
    if matches!(read_runtime_port_file(runtime_port_file), Ok(Some(port)) if port == owned_port) {
        clear_runtime_port_file(runtime_port_file);
    }
}

fn with_startup_error(
    mut runtime: BackendRuntime,
    startup_error: Option<String>,
) -> BackendRuntime {
    runtime.startup_error = startup_error.or(runtime.startup_error);
    runtime
}

fn runtime_from_port_file_port(port: u16) -> BackendRuntime {
    BackendRuntime {
        port,
        backend_url: format!("http://127.0.0.1:{port}"),
        ws_url: format!("ws://127.0.0.1:{port}/ws"),
        startup_error: None,
    }
}

fn read_runtime_port_file(runtime_port_file: &Path) -> Result<Option<u16>, String> {
    match fs::read_to_string(runtime_port_file) {
        Ok(raw) => {
            let trimmed = raw.trim();
            if trimmed.is_empty() {
                return Ok(None);
            }
            let port = trimmed
                .parse::<u16>()
                .map_err(|e| format!("解析 runtime 端口文件失败: {e}"))?;
            if port == 0 {
                return Err("runtime 端口文件缺少有效端口".to_string());
            }
            Ok(Some(port))
        }
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(err) => Err(format!("读取 runtime 端口文件失败: {err}")),
    }
}

fn resolve_sidecar_runtime(
    timeout: Duration,
    runtime_port_file: &Path,
) -> Result<BackendRuntime, String> {
    let deadline = Instant::now() + timeout;
    let mut last_err = String::from("等待 authoritative runtime port file 就绪");

    while Instant::now() < deadline {
        match read_runtime_port_file(runtime_port_file) {
            Ok(Some(port)) => return Ok(runtime_from_port_file_port(port)),
            Ok(None) => {
                last_err = String::from("等待 authoritative runtime port file 就绪");
            }
            Err(err) => last_err = err,
        }
        thread::sleep(Duration::from_millis(120));
    }

    Err(last_err)
}

fn spawn_sidecar(
    app: &tauri::AppHandle,
    runtime_home: &Path,
    assets_root: &Path,
) -> Result<CommandChild, Box<dyn Error>> {
    let sidecar = app
        .shell()
        .sidecar("carto-agent")
        .map_err(|e| build_error(format!("准备 carto-agent sidecar 失败: {e}")))?
        .env(RUNTIME_HOME_ENV_VAR, runtime_home)
        .env(ASSETS_ROOT_ENV_VAR, assets_root)
        .env(DESKTOP_PARENT_PID_ENV_VAR, std::process::id().to_string());
    let (_rx, child) = sidecar
        .spawn()
        .map_err(|e| build_error(format!("启动 carto-agent sidecar 失败: {e}")))?;
    Ok(child)
}

#[cfg(unix)]
fn terminate_pid(pid: u32) -> Result<(), String> {
    let status = Command::new("kill")
        .args(["-TERM", &pid.to_string()])
        .status()
        .map_err(|e| format!("终止进程失败 (pid={pid}): {e}"))?;
    if !status.success() {
        return Err(format!("终止进程失败 (pid={pid}), exit={status}"));
    }
    Ok(())
}

#[cfg(windows)]
fn terminate_pid(pid: u32) -> Result<(), String> {
    let status = Command::new("taskkill")
        .args(["/PID", &pid.to_string(), "/T"])
        .status()
        .map_err(|e| format!("终止进程失败 (pid={pid}): {e}"))?;
    if !status.success() {
        return Err(format!("终止进程失败 (pid={pid}), exit={status}"));
    }
    Ok(())
}

#[cfg(windows)]
fn force_kill_pid(pid: u32) -> Result<(), String> {
    let status = Command::new("taskkill")
        .args(["/PID", &pid.to_string(), "/T", "/F"])
        .status()
        .map_err(|e| format!("强制终止进程失败 (pid={pid}): {e}"))?;
    if !status.success() {
        return Err(format!("强制终止进程失败 (pid={pid}), exit={status}"));
    }
    Ok(())
}

#[cfg(unix)]
fn force_kill_pid(pid: u32) -> Result<(), String> {
    let status = Command::new("kill")
        .args(["-KILL", &pid.to_string()])
        .status()
        .map_err(|e| format!("强制终止进程失败 (pid={pid}): {e}"))?;
    if !status.success() {
        return Err(format!("强制终止进程失败 (pid={pid}), exit={status}"));
    }
    Ok(())
}

fn terminate_sidecar_child(child: CommandChild) {
    let pid = child.pid();
    if terminate_pid(pid).is_err() {
        let _ = force_kill_pid(pid);
    }
    thread::sleep(Duration::from_millis(1200));
    let _ = child.kill();
}

fn start_managed_sidecar(app: &tauri::AppHandle) -> Result<ManagedSidecar, Box<dyn Error>> {
    let prepared = prepare_sidecar_launch(app).map_err(build_error)?;
    clear_runtime_port_file(&prepared.runtime_port_file);

    fs::create_dir_all(prepared.runtime_home.join("config"))
        .map_err(|e| build_error(format!("创建 runtime config 目录失败: {e}")))?;
    fs::create_dir_all(prepared.runtime_home.join("data"))
        .map_err(|e| build_error(format!("创建 runtime data 目录失败: {e}")))?;

    let child = spawn_sidecar(app, &prepared.runtime_home, &prepared.assets_root)?;
    let runtime = match resolve_sidecar_runtime(SIDECAR_READY_TIMEOUT, &prepared.runtime_port_file)
    {
        Ok(runtime) => runtime,
        Err(err) => {
            terminate_sidecar_child(child);
            return Err(build_error(err));
        }
    };

    Ok(ManagedSidecar {
        child,
        runtime,
        runtime_port_file: prepared.runtime_port_file,
    })
}

fn shutdown_sidecar(app_handle: &tauri::AppHandle) {
    if let Some(state) = app_handle.try_state::<SidecarState>() {
        if let Ok(mut guard) = state.0.lock() {
            if let Some(managed) = guard.take() {
                terminate_sidecar_child(managed.child);
                remove_runtime_port_file(&managed.runtime_port_file, managed.runtime.port);
                return;
            }
        }
    }
}

fn normalize_optional_input(value: Option<String>) -> Option<String> {
    value.and_then(|raw| {
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed.to_string())
        }
    })
}

fn normalize_qgis_executable_path(path: &str) -> String {
    let trimmed = path.trim();
    #[cfg(target_os = "macos")]
    {
        if trimmed.ends_with(".app") {
            return format!("{trimmed}/Contents/MacOS/QGIS");
        }
    }
    trimmed.to_string()
}

fn detect_qgis_executable_path() -> Option<String> {
    #[cfg(target_os = "macos")]
    let candidates = vec![
        "/Applications/QGIS.app/Contents/MacOS/QGIS",
        "/Applications/QGIS-LTR.app/Contents/MacOS/QGIS",
        "/Applications/QGIS-Dev.app/Contents/MacOS/QGIS",
        "/opt/homebrew/bin/qgis",
        "/usr/local/bin/qgis",
    ];

    #[cfg(target_os = "windows")]
    let candidates = vec![
        "C:\\Program Files\\QGIS 3.40\\bin\\qgis-bin.exe",
        "C:\\Program Files\\QGIS 3.38\\bin\\qgis-bin.exe",
        "C:\\Program Files\\QGIS 3.36\\bin\\qgis-bin.exe",
        "C:\\Program Files\\QGIS 3.34\\bin\\qgis-bin.exe",
        "C:\\OSGeo4W\\bin\\qgis-bin.exe",
    ];

    #[cfg(target_os = "linux")]
    let candidates = vec!["/usr/bin/qgis", "/usr/local/bin/qgis", "/snap/bin/qgis"];

    for candidate in candidates {
        if Path::new(candidate).exists() {
            return Some(candidate.to_string());
        }
    }

    #[cfg(not(target_os = "windows"))]
    if let Ok(output) = Command::new("which").arg("qgis").output() {
        if output.status.success() {
            let candidate = String::from_utf8_lossy(&output.stdout).trim().to_string();
            if !candidate.is_empty() && Path::new(&candidate).exists() {
                return Some(candidate);
            }
        }
    }

    #[cfg(target_os = "windows")]
    if let Ok(output) = Command::new("where").arg("qgis-bin.exe").output() {
        if output.status.success() {
            let candidate = String::from_utf8_lossy(&output.stdout)
                .lines()
                .next()
                .unwrap_or("")
                .trim()
                .to_string();
            if !candidate.is_empty() && Path::new(&candidate).exists() {
                return Some(candidate);
            }
        }
    }

    None
}

fn resolve_qgis_executable_path(executable_path: Option<String>) -> Result<String, String> {
    if let Some(configured) = normalize_optional_input(executable_path) {
        let normalized = normalize_qgis_executable_path(&configured);
        if Path::new(&normalized).exists() {
            return Ok(normalized);
        }
        return Err(format!("QGIS 路径不存在: {normalized}"));
    }

    detect_qgis_executable_path()
        .ok_or_else(|| "未检测到 QGIS 可执行文件，请前往设置-常规设置配置 QGIS 路径".to_string())
}

fn dialog_file_path_to_string(file_path: FilePath) -> String {
    match file_path {
        FilePath::Path(path) => path.to_string_lossy().to_string(),
        FilePath::Url(url) => url
            .to_file_path()
            .map(|path| path.to_string_lossy().to_string())
            .unwrap_or_else(|_| url.to_string()),
    }
}

async fn pick_dialog_file(
    app: tauri::AppHandle,
    filter: Option<(&'static str, &'static [&'static str])>,
    error_context: &str,
) -> Result<Option<String>, String> {
    let (tx, rx) = std::sync::mpsc::channel();

    if let Some((name, extensions)) = filter {
        app.dialog()
            .file()
            .add_filter(name, extensions)
            .pick_file(move |selected| {
                let _ = tx.send(selected);
            });
    } else {
        app.dialog().file().pick_file(move |selected| {
            let _ = tx.send(selected);
        });
    }

    let selected = tauri::async_runtime::spawn_blocking(move || rx.recv())
        .await
        .map_err(|e| format!("{error_context}: 等待文件对话框结果失败: {e}"))?
        .map_err(|e| format!("{error_context}: {e}"))?;

    Ok(selected.map(dialog_file_path_to_string))
}

async fn pick_dialog_directory(
    app: tauri::AppHandle,
    error_context: &str,
) -> Result<Option<String>, String> {
    let (tx, rx) = std::sync::mpsc::channel();

    app.dialog().file().pick_folder(move |selected| {
        let _ = tx.send(selected);
    });

    let selected = tauri::async_runtime::spawn_blocking(move || rx.recv())
        .await
        .map_err(|e| format!("{error_context}: 等待目录对话框结果失败: {e}"))?
        .map_err(|e| format!("{error_context}: {e}"))?;

    Ok(selected.map(dialog_file_path_to_string))
}

#[tauri::command]
fn detect_qgis_executable() -> Result<String, String> {
    detect_qgis_executable_path()
        .ok_or_else(|| "未检测到 QGIS 可执行文件，请前往设置-常规设置配置 QGIS 路径".to_string())
}

#[cfg(target_os = "macos")]
fn resolve_app_bundle_path(executable: &str) -> Option<String> {
    if executable.ends_with(".app") {
        return Some(executable.to_string());
    }

    executable
        .find(".app/")
        .map(|index| executable[..index + ".app".len()].to_string())
}

#[cfg(target_os = "macos")]
fn resolve_default_qgis_plugins_dir() -> Result<PathBuf, String> {
    let home = std::env::var_os("HOME")
        .map(PathBuf::from)
        .ok_or_else(|| "无法解析用户主目录".to_string())?;
    let plugins_dir = home
        .join("Library")
        .join("Application Support")
        .join("QGIS")
        .join("QGIS3")
        .join("profiles")
        .join("default")
        .join("python")
        .join("plugins");

    if !plugins_dir.exists() {
        return Err(format!("未找到 QGIS 插件目录: {}", plugins_dir.display()));
    }
    let autocarto_dir = plugins_dir.join("autocarto");
    if !autocarto_dir.exists() {
        return Err(format!(
            "未找到 AutoCarto 插件目录: {}",
            autocarto_dir.display()
        ));
    }
    Ok(plugins_dir)
}

#[cfg(all(target_os = "macos", debug_assertions))]
fn sync_development_qgis_plugin(assets_root: &Path, plugins_dir: &Path) -> Result<(), String> {
    let installed_plugin = plugins_dir.join("autocarto");
    let source = assets_root
        .canonicalize()
        .map_err(|e| format!("无法定位 AutoCarto 开发插件源码: {e}"))?;
    let installed = installed_plugin
        .canonicalize()
        .map_err(|e| format!("无法定位已安装的 AutoCarto 插件: {e}"))?;
    if source == installed {
        return Ok(());
    }

    // 直接运行 `tauri dev` 时也要更新 QGIS 实际加载的插件副本。
    let status = Command::new("rsync")
        .args([
            "-a",
            "--exclude=.git",
            "--exclude=tests",
            "--exclude=logs",
            "--exclude=.session",
            "--exclude=__pycache__",
            "--exclude=*.pyc",
        ])
        .arg(format!("{}/", source.display()))
        .arg(format!("{}/", installed.display()))
        .status()
        .map_err(|e| format!("同步 AutoCarto 开发插件失败: {e}"))?;
    if !status.success() {
        return Err(format!(
            "同步 AutoCarto 开发插件失败，rsync 退出状态: {status}"
        ));
    }
    Ok(())
}

#[cfg(target_os = "macos")]
fn prepare_managed_qgis_profile() -> Result<(PathBuf, String), String> {
    let profiles_root = resolve_plugin_data_dir()?.join("qgis-profiles");
    let profile_suffix = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    let profile_name = format!("autocarto-instance-{profile_suffix}");
    let profile_dir = profiles_root.join("profiles").join(&profile_name);
    let ini_dir = profile_dir.join("qgis.org");
    fs::create_dir_all(&ini_dir).map_err(|e| format!("创建托管 QGIS profile 失败: {e}"))?;

    let ini_path = ini_dir.join("QGIS3.ini");
    fs::write(
        &ini_path,
        "[PythonPlugins]
autocarto=true
",
    )
    .map_err(|e| format!("写入托管 QGIS profile 配置失败: {e}"))?;

    Ok((profiles_root, profile_name))
}

#[cfg(target_os = "macos")]
fn launch_qgis_macos_new_instance(
    executable: &str,
    project: Option<&str>,
    runtime_home: &Path,
    assets_root: &Path,
) -> Result<(), String> {
    let plugin_dir = resolve_default_qgis_plugins_dir()?;
    #[cfg(debug_assertions)]
    sync_development_qgis_plugin(assets_root, &plugin_dir)?;
    let (profiles_root, profile_name) = prepare_managed_qgis_profile()?;
    let mut command = Command::new(executable);
    command
        .arg("--profiles-path")
        .arg(&profiles_root)
        .arg("--profile")
        .arg(&profile_name)
        .env("QGIS_PLUGINPATH", &plugin_dir)
        .env(RUNTIME_HOME_ENV_VAR, runtime_home)
        .env(ASSETS_ROOT_ENV_VAR, assets_root);

    if let Some(project_path) = project {
        command.arg("--project").arg(project_path);
    }

    command
        .spawn()
        .map_err(|e| format!("启动新的 QGIS 实例失败: {e}"))?;

    Ok(())
}

fn launch_qgis_direct(
    executable: &str,
    project: Option<&str>,
    runtime_home: &Path,
    assets_root: &Path,
) -> Result<(), String> {
    let mut command = Command::new(executable);
    command
        .env(RUNTIME_HOME_ENV_VAR, runtime_home)
        .env(ASSETS_ROOT_ENV_VAR, assets_root);
    if let Some(project_path) = project {
        command.arg("--project").arg(project_path);
    }

    command
        .spawn()
        .map_err(|e| format!("启动 QGIS 失败: {e}"))?;

    Ok(())
}

#[cfg(target_os = "macos")]
fn launch_qgis_macos(
    app: &tauri::AppHandle,
    executable: &str,
    project: Option<&str>,
    force_new_instance: bool,
) -> Result<(), String> {
    let prepared = prepare_sidecar_launch(app)?;
    if force_new_instance {
        return launch_qgis_macos_new_instance(
            executable,
            project,
            &prepared.runtime_home,
            &prepared.assets_root,
        );
    }

    if let Some(app_bundle) = resolve_app_bundle_path(executable) {
        let mut command = Command::new("open");
        command
            .arg("-a")
            .arg(&app_bundle)
            .env(RUNTIME_HOME_ENV_VAR, &prepared.runtime_home)
            .env(ASSETS_ROOT_ENV_VAR, &prepared.assets_root);

        if let Some(project_path) = project {
            command.arg(project_path);
        }

        command
            .spawn()
            .map_err(|e| format!("通过 open 命令启动 QGIS 失败: {e}"))?;

        Ok(())
    } else {
        launch_qgis_direct(
            executable,
            project,
            &prepared.runtime_home,
            &prepared.assets_root,
        )
    }
}

#[tauri::command]
fn launch_qgis(
    app: tauri::AppHandle,
    executable_path: Option<String>,
    project_path: Option<String>,
    _force_new_instance: Option<bool>,
) -> Result<String, String> {
    let executable = resolve_qgis_executable_path(executable_path)?;
    let project = normalize_optional_input(project_path);
    #[cfg(target_os = "macos")]
    let force_new_instance = _force_new_instance.unwrap_or(false);

    if let Some(project_path) = project.as_deref() {
        if !Path::new(project_path).exists() {
            return Err(format!("工程文件不存在: {project_path}"));
        }
    }

    #[cfg(target_os = "macos")]
    {
        launch_qgis_macos(&app, &executable, project.as_deref(), force_new_instance)?;
    }

    #[cfg(not(target_os = "macos"))]
    {
        let prepared = prepare_sidecar_launch(&app)?;
        launch_qgis_direct(
            &executable,
            project.as_deref(),
            &prepared.runtime_home,
            &prepared.assets_root,
        )?;
    }

    Ok(executable)
}

#[tauri::command]
async fn pick_qgis_project(app: tauri::AppHandle) -> Result<Option<String>, String> {
    pick_dialog_file(
        app,
        Some(("QGIS 工程", &["qgs", "qgz"])),
        "选择 QGIS 工程文件失败",
    )
    .await
}

#[tauri::command]
async fn open_local_path(path: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        local_output::validate_input(&path)?;
        let resolved = resolve_project_path(&path)?;
        let target = local_output::validate_existing(&resolved)?;
        local_output::open(&target)
    })
    .await
    .map_err(|error| format!("打开成果失败：{error}"))?
}

#[tauri::command]
async fn pick_qgis_project_directory(app: tauri::AppHandle) -> Result<Option<String>, String> {
    pick_dialog_directory(app, "选择 QGIS 工程目录失败").await
}

#[tauri::command]
async fn pick_file_path(app: tauri::AppHandle) -> Result<Option<String>, String> {
    pick_dialog_file(app, None, "选择文件失败").await
}

#[tauri::command]
async fn pick_directory_path(app: tauri::AppHandle) -> Result<Option<String>, String> {
    pick_dialog_directory(app, "选择目录失败").await
}

#[tauri::command]
async fn pick_qgis_executable(app: tauri::AppHandle) -> Result<Option<String>, String> {
    pick_dialog_file(app, None, "选择 QGIS 可执行文件失败").await
}

#[tauri::command]
fn get_backend_runtime(app: tauri::AppHandle) -> Result<BackendRuntime, String> {
    let startup_error = app
        .try_state::<StartupState>()
        .and_then(|state| state.0.lock().ok().and_then(|guard| guard.clone()));

    if let Some(state) = app.try_state::<SidecarState>() {
        if let Ok(guard) = state.0.lock() {
            if let Some(managed) = guard.as_ref() {
                if let Ok(Some(port)) = read_runtime_port_file(&managed.runtime_port_file) {
                    return Ok(with_startup_error(
                        runtime_from_port_file_port(port),
                        startup_error,
                    ));
                }
                return Ok(with_startup_error(managed.runtime.clone(), startup_error));
            }
        }
    }

    if let Some(message) = startup_error {
        return Err(message);
    }
    Err(
        "desktop runtime port file is unavailable; wait for carto-agent to write data/.port"
            .to_string(),
    )
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    configure_default_runtime_environment();

    let app = tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            detect_qgis_executable,
            launch_qgis,
            pick_qgis_project,
            pick_qgis_project_directory,
            pick_file_path,
            pick_directory_path,
            pick_qgis_executable,
            open_local_path,
            get_backend_runtime,
        ])
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            let app_handle = app.handle().clone();
            app.manage(SidecarState(Mutex::new(None)));
            app.manage(StartupState(Mutex::new(None)));

            match start_managed_sidecar(&app_handle) {
                Ok(child) => {
                    if let Some(state) = app.try_state::<SidecarState>() {
                        if let Ok(mut guard) = state.0.lock() {
                            *guard = Some(child);
                        }
                    }
                    if let Some(state) = app.try_state::<StartupState>() {
                        if let Ok(mut guard) = state.0.lock() {
                            *guard = None;
                        }
                    }
                }
                Err(err) => {
                    let message = format!("启动后端失败: {err}");
                    eprintln!("{message}");
                    if let Some(state) = app.try_state::<StartupState>() {
                        if let Ok(mut guard) = state.0.lock() {
                            *guard = Some(message);
                        }
                    }
                }
            }

            #[cfg(debug_assertions)]
            {
                let window = app.get_webview_window("main").unwrap();
                window.open_devtools();
            }

            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { .. } = event {
                shutdown_sidecar(&window.app_handle());
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|app_handle, event| {
        if matches!(event, tauri::RunEvent::Exit) {
            shutdown_sidecar(app_handle);
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::{Path, PathBuf};
    use std::sync::Mutex;
    use std::time::{SystemTime, UNIX_EPOCH};

    static ENV_AND_CWD_LOCK: Mutex<()> = Mutex::new(());

    fn make_temp_root(prefix: &str) -> PathBuf {
        let unique = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos();
        let path = std::env::temp_dir().join(format!("{prefix}-{unique}"));
        fs::create_dir_all(&path).expect("create temp root");
        path
    }

    fn create_plugin_root(base: &Path) -> PathBuf {
        let plugin_root = base.join("autocarto");
        fs::create_dir_all(plugin_root.join("skills").join("system"))
            .expect("create system skills");
        fs::create_dir_all(plugin_root.join("skills").join("user")).expect("create user skills");
        plugin_root
    }

    #[test]
    fn runtime_home_is_project_relative_and_independent_of_cwd() {
        let _lock = ENV_AND_CWD_LOCK.lock().expect("lock env and cwd");
        let root = make_temp_root("autocarto-project-home");
        let previous_root = std::env::var_os(PROJECT_ROOT_ENV_VAR);
        let original_cwd = std::env::current_dir().unwrap();
        std::env::set_var(PROJECT_ROOT_ENV_VAR, &root);
        let default = resolve_runtime_home_from_env(None).unwrap();
        let relative = resolve_runtime_home_from_env(Some("custom/runtime")).unwrap();
        std::env::set_current_dir(std::env::temp_dir()).unwrap();
        let after = resolve_runtime_home_from_env(Some("custom/runtime")).unwrap();
        std::env::set_current_dir(original_cwd).unwrap();
        match previous_root {
            Some(value) => std::env::set_var(PROJECT_ROOT_ENV_VAR, value),
            None => std::env::remove_var(PROJECT_ROOT_ENV_VAR),
        }
        assert_eq!(default, root.join("autocarto_data"));
        assert_eq!(relative, root.join("custom/runtime"));
        assert_eq!(after, relative);
        assert!(!default.exists());
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn local_output_resolves_relative_to_project_from_any_cwd() {
        let _lock = ENV_AND_CWD_LOCK.lock().expect("lock env and cwd");
        let root = make_temp_root("enio2-local-output");
        fs::create_dir_all(root.join("outputs")).unwrap();
        fs::write(root.join("outputs/map.pdf"), "result").unwrap();
        let previous_root = std::env::var_os(PROJECT_ROOT_ENV_VAR);
        let previous_cwd = std::env::current_dir().unwrap();
        std::env::set_var(PROJECT_ROOT_ENV_VAR, &root);
        std::env::set_current_dir(std::env::temp_dir()).unwrap();
        let resolved = resolve_project_path("outputs/map.pdf").unwrap();
        let target = local_output::validate_existing(&resolved).unwrap();
        std::env::set_current_dir(previous_cwd).unwrap();
        match previous_root {
            Some(value) => std::env::set_var(PROJECT_ROOT_ENV_VAR, value),
            None => std::env::remove_var(PROJECT_ROOT_ENV_VAR),
        }
        assert_eq!(target, root.join("outputs/map.pdf").canonicalize().unwrap());
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn runtime_port_file_path_uses_env_override_under_data_dir() {
        let _lock = ENV_AND_CWD_LOCK.lock().expect("lock env and cwd");
        let custom_home_parent = make_temp_root("autocarto-tauri-env");
        let custom_home = custom_home_parent.join("custom-home");
        fs::create_dir_all(custom_home.join("data")).expect("create data dir");
        let original_home = std::env::var_os(RUNTIME_HOME_ENV_VAR);
        std::env::set_var(RUNTIME_HOME_ENV_VAR, &custom_home);

        let port_file = runtime_port_file_path().expect("resolve runtime port file");

        match original_home {
            Some(value) => std::env::set_var(RUNTIME_HOME_ENV_VAR, value),
            None => std::env::remove_var(RUNTIME_HOME_ENV_VAR),
        }
        let _ = fs::remove_dir_all(Path::new(&custom_home_parent));

        assert_eq!(port_file, custom_home.join("data").join(".port"));
    }

    #[test]
    fn resolve_assets_root_rejects_invalid_env_override_without_system_skills() {
        let _lock = ENV_AND_CWD_LOCK.lock().expect("lock env and cwd");
        let custom_home_parent = make_temp_root("autocarto-tauri-invalid-env");
        let custom_home = custom_home_parent.join("custom-assets");
        fs::create_dir_all(&custom_home).expect("create custom assets root");
        let original_assets_root = std::env::var_os(ASSETS_ROOT_ENV_VAR);
        std::env::set_var(ASSETS_ROOT_ENV_VAR, &custom_home);

        let err = resolve_assets_root().expect_err("invalid assets root override must be rejected");

        match original_assets_root {
            Some(value) => std::env::set_var(ASSETS_ROOT_ENV_VAR, value),
            None => std::env::remove_var(ASSETS_ROOT_ENV_VAR),
        }
        let _ = fs::remove_dir_all(Path::new(&custom_home_parent));

        assert!(err.contains(ASSETS_ROOT_ENV_VAR), "unexpected error: {err}");
        assert!(err.contains("skills/system"), "unexpected error: {err}");
    }

    #[test]
    fn prepare_sidecar_launch_reports_discovery_failure_details() {
        let root_dir = make_temp_root("autocarto-tauri-missing-root");
        let cwd_seed = root_dir.join("desktop").join("cwd");
        let exec_seed = root_dir.join("desktop").join("bin");
        let runtime_home = root_dir.join("app-local");

        let err = prepare_sidecar_launch_from_sources(
            Some(runtime_home.to_str().expect("runtime home utf-8")),
            None,
            Some(cwd_seed),
            Some(exec_seed),
        )
        .expect_err("missing plugin root should fail during preflight");

        let _ = fs::remove_dir_all(Path::new(&root_dir));

        assert!(err.contains(ASSETS_ROOT_ENV_VAR), "unexpected error: {err}");
        assert!(err.contains("当前目录"), "unexpected error: {err}");
        assert!(err.contains("可执行"), "unexpected error: {err}");
        assert!(err.contains("8 层"), "unexpected error: {err}");
        assert!(err.contains("skills/system"), "unexpected error: {err}");
    }

    #[test]
    fn runtime_port_file_path_rejects_invalid_env_override() {
        let _lock = ENV_AND_CWD_LOCK.lock().expect("lock env and cwd");
        let custom_home_parent = make_temp_root("autocarto-tauri-invalid-port-home");
        let custom_home = custom_home_parent.join("custom-home");
        fs::create_dir_all(custom_home.join("data")).expect("create data dir");
        let original_home = std::env::var_os(RUNTIME_HOME_ENV_VAR);
        std::env::set_var(RUNTIME_HOME_ENV_VAR, &custom_home);

        let port_file =
            runtime_port_file_path().expect("runtime home should allow authoritative port path");

        match original_home {
            Some(value) => std::env::set_var(RUNTIME_HOME_ENV_VAR, value),
            None => std::env::remove_var(RUNTIME_HOME_ENV_VAR),
        }
        let _ = fs::remove_dir_all(Path::new(&custom_home_parent));

        assert_eq!(port_file, custom_home.join("data").join(".port"));
    }

    #[test]
    fn read_runtime_port_file_reads_authoritative_port() {
        let _lock = ENV_AND_CWD_LOCK.lock().expect("lock env and cwd");
        let custom_home_parent = make_temp_root("autocarto-tauri-port-read");
        let custom_home = custom_home_parent.join("custom-home");
        fs::create_dir_all(custom_home.join("data")).expect("create data dir");
        fs::write(custom_home.join("data").join(".port"), "41234").expect("write port file");
        let original_home = std::env::var_os(RUNTIME_HOME_ENV_VAR);
        std::env::set_var(RUNTIME_HOME_ENV_VAR, &custom_home);

        let port_file = runtime_port_file_path().expect("resolve runtime port file");
        let port = read_runtime_port_file(&port_file).expect("read runtime port file");

        match original_home {
            Some(value) => std::env::set_var(RUNTIME_HOME_ENV_VAR, value),
            None => std::env::remove_var(RUNTIME_HOME_ENV_VAR),
        }
        let _ = fs::remove_dir_all(Path::new(&custom_home_parent));

        assert_eq!(port, Some(41234));
    }

    #[test]
    fn read_runtime_port_file_returns_none_when_missing() {
        let _lock = ENV_AND_CWD_LOCK.lock().expect("lock env and cwd");
        let custom_home_parent = make_temp_root("autocarto-tauri-port-missing");
        let custom_home = custom_home_parent.join("custom-home");
        fs::create_dir_all(custom_home.join("data")).expect("create data dir");
        let original_home = std::env::var_os(RUNTIME_HOME_ENV_VAR);
        std::env::set_var(RUNTIME_HOME_ENV_VAR, &custom_home);

        let port_file = runtime_port_file_path().expect("resolve runtime port file");
        let port = read_runtime_port_file(&port_file).expect("read runtime port file");

        match original_home {
            Some(value) => std::env::set_var(RUNTIME_HOME_ENV_VAR, value),
            None => std::env::remove_var(RUNTIME_HOME_ENV_VAR),
        }
        let _ = fs::remove_dir_all(Path::new(&custom_home_parent));

        assert_eq!(port, None);
    }

    #[test]
    fn runtime_locator_and_supervisor_prefers_bundled_root_with_app_local_runtime_port_file() {
        let root_dir = make_temp_root("autocarto-tauri-independent-runtime");
        let bundled_root = create_plugin_root(&root_dir.join("bundle"));
        let app_local = root_dir.join("app-local");
        fs::create_dir_all(app_local.join("config")).expect("create app local config");
        fs::create_dir_all(app_local.join("data")).expect("create app local data");

        let app_local_str = app_local.to_str().expect("app local path must be utf-8");
        let prepared = prepare_sidecar_launch_from_sources(
            Some(app_local_str),
            None,
            None,
            Some(bundled_root.join("src-tauri").join("bin")),
        )
        .expect("runtime locator should accept app-local runtime data alongside bundled resources");

        assert_eq!(prepared.runtime_home, app_local);
        assert_eq!(prepared.assets_root, bundled_root);
        assert_eq!(
            prepared.runtime_port_file,
            app_local.join("data").join(".port")
        );
    }

    #[test]
    fn runtime_locator_and_supervisor_clears_stale_port_file_before_startup() {
        let root_dir = make_temp_root("autocarto-tauri-runtime-port-preserve");
        let runtime_port_file = root_dir.join(".port");
        fs::write(&runtime_port_file, "50301").expect("write authoritative runtime port file");

        clear_runtime_port_file(&runtime_port_file);

        assert!(!runtime_port_file.exists());
    }

    #[test]
    fn runtime_locator_and_supervisor_removes_port_file_on_shutdown() {
        let root_dir = make_temp_root("autocarto-tauri-runtime-port-shutdown");
        let runtime_port_file = root_dir.join(".port");
        fs::write(&runtime_port_file, "50301").expect("write authoritative runtime port file");

        remove_runtime_port_file(&runtime_port_file, 50301);

        assert!(!runtime_port_file.exists());
    }

    #[test]
    fn runtime_locator_and_supervisor_preserves_replacement_port_on_shutdown() {
        let root_dir = make_temp_root("autocarto-tauri-runtime-port-newer");
        let runtime_port_file = root_dir.join(".port");
        fs::write(&runtime_port_file, "50302").expect("write replacement runtime port file");

        remove_runtime_port_file(&runtime_port_file, 50301);

        assert_eq!(fs::read_to_string(&runtime_port_file).unwrap(), "50302");
        let _ = fs::remove_dir_all(&root_dir);
    }
}
