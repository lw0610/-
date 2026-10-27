use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

pub fn validate_input(raw: &str) -> Result<(), String> {
    let value = raw.trim();
    if value.is_empty() || value.contains('\0') {
        return Err("成果路径为空或无效".into());
    }
    // A Windows drive letter is a path. Every other URI scheme is rejected.
    if let Some(index) = value.find(':') {
        if index != 1 || !value.as_bytes()[0].is_ascii_alphabetic() {
            return Err("只能打开本地成果路径，不能打开 URL".into());
        }
    }
    if value.contains("://") {
        return Err("只能打开本地成果路径，不能打开 URL".into());
    }
    Ok(())
}

pub fn validate_existing(path: &Path) -> Result<PathBuf, String> {
    let target = fs::canonicalize(path).map_err(|_| "成果路径不存在或无法读取".to_string())?;
    let blocked = ["app", "appx", "appxbundle", "bundle", "framework"];
    if target.components().any(|part| {
        Path::new(part.as_os_str())
            .extension()
            .and_then(|ext| ext.to_str())
            .map(|ext| blocked.contains(&ext.to_ascii_lowercase().as_str()))
            .unwrap_or(false)
    }) {
        return Err("不能打开应用包或应用包内部路径".into());
    }
    let metadata = fs::metadata(&target).map_err(|_| "无法读取成果路径".to_string())?;
    if metadata.is_dir() {
        return Ok(target);
    }
    if !metadata.is_file() {
        return Err("只能打开普通成果文件或目录".into());
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        if metadata.permissions().mode() & 0o111 != 0 {
            return Err("不能打开可执行文件".into());
        }
    }
    let extension = target
        .extension()
        .and_then(|ext| ext.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    let allowed = [
        "png", "jpg", "jpeg", "gif", "webp", "tif", "tiff", "svg", "pdf", "csv", "tsv", "xls",
        "xlsx", "ods", "geojson", "json", "gpkg", "shp", "dbf", "kml", "kmz", "qgs", "qgz", "qml",
        "qpt", "txt", "md", "yaml", "yml", "xml", "zip", "docx", "pptx",
    ];
    if !allowed.contains(&extension.as_str()) {
        return Err("不支持打开此类文件；可执行文件、脚本和应用包不允许打开".into());
    }
    Ok(target)
}

pub fn open(target: &Path) -> Result<(), String> {
    // Use the platform file handler with a single canonical path argument.
    // Directory targets are handled by the system file manager. No shell runs.
    #[cfg(target_os = "macos")]
    let mut command = {
        let mut command = Command::new("/usr/bin/open");
        command.arg("--");
        command
    };
    #[cfg(target_os = "windows")]
    let mut command = Command::new("explorer.exe");
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    let mut command = Command::new("xdg-open");
    let status = command
        .arg(target)
        .status()
        .map_err(|error| format!("打开成果失败：{error}"))?;
    if !status.success() {
        return Err("系统未能打开此成果，请检查文件关联".into());
    }
    Ok(())
}

pub fn reveal(target: &Path) -> Result<(), String> {
    if target.is_dir() { return open(target); }
    #[cfg(target_os = "macos")]
    let status = Command::new("/usr/bin/open").arg("-R").arg("--").arg(target).status();
    #[cfg(target_os = "windows")]
    let status = Command::new("explorer.exe").arg(format!("/select,{}", target.display())).status();
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    let status = Command::new("xdg-open").arg(target.parent().ok_or("文件没有所在目录")?).status();
    if !status.map_err(|error| format!("显示文件位置失败：{error}"))?.success() { return Err("系统未能显示文件位置".into()); }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};
    fn temp() -> PathBuf {
        let path = std::env::temp_dir().join(format!(
            "enio2-output-{}",
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir_all(&path).unwrap();
        path
    }
    #[test]
    fn accepts_regular_results_and_directories_but_rejects_urls_and_missing_paths() {
        let root = temp();
        for name in [
            "地图.png",
            "report.pdf",
            "data.csv",
            "data.geojson",
            "data.gpkg",
            "project.qgz",
            "run.json",
            "readme.txt",
        ] {
            let path = root.join(name);
            fs::write(&path, "result").unwrap();
            assert_eq!(
                validate_existing(&path).unwrap(),
                path.canonicalize().unwrap()
            );
        }
        assert!(validate_existing(&root).is_ok());
        assert!(validate_existing(&root.join("missing.pdf")).is_err());
        for value in [
            "https://example.org/a.pdf",
            "file:///tmp/a.pdf",
            "javascript:alert(1)",
            "data:hello",
            "",
            "a\0b",
        ] {
            assert!(validate_input(value).is_err());
        }
        for value in ["outputs/地图.pdf", "/tmp/a.pdf", "C:\\outputs\\map.pdf"] {
            assert!(validate_input(value).is_ok());
        }
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn rejects_scripts_executables_and_application_packages() {
        let root = temp();
        for name in [
            "a.py",
            "a.sh",
            "a.js",
            "a.exe",
            "a.bat",
            "a.cmd",
            "a.desktop",
            "a.ps1",
            "a.html",
            "no-extension",
        ] {
            let path = root.join(name);
            fs::write(&path, "script").unwrap();
            assert!(validate_existing(&path).is_err());
        }
        let app = root.join("Test.app");
        fs::create_dir_all(&app).unwrap();
        fs::write(app.join("inside.pdf"), "result").unwrap();
        assert!(validate_existing(&app).is_err());
        assert!(validate_existing(&app.join("inside.pdf")).is_err());
        #[cfg(unix)]
        {
            use std::os::unix::fs::{symlink, PermissionsExt};
            let binary = root.join("fake.png");
            fs::write(&binary, "executable").unwrap();
            fs::set_permissions(&binary, fs::Permissions::from_mode(0o755)).unwrap();
            assert!(validate_existing(&binary).is_err());
            symlink(root.join("a.sh"), root.join("alias.pdf")).unwrap();
            assert!(validate_existing(&root.join("alias.pdf")).is_err());
        }
        fs::remove_dir_all(root).unwrap();
    }
}
