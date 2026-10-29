use std::path::{Path, PathBuf};

#[derive(Debug, PartialEq)]
pub(crate) struct PathCorrection {
    pub variable: &'static str,
    pub previous: PathBuf,
    pub replacement: PathBuf,
}

#[derive(Debug, PartialEq)]
pub(crate) struct DevelopmentPathCorrections {
    pub project_root: PathBuf,
    pub paths: Vec<PathCorrection>,
}

pub(crate) fn inherited_checkout_path_corrections(
    development_mode: bool,
    source_seed: &Path,
    explicit_project_root: Option<&str>,
    runtime_home: Option<&str>,
    assets_root: Option<&str>,
) -> Option<DevelopmentPathCorrections> {
    // An explicit project root still selects a deliberate deployment. Only
    // development launches without that anchor repair inherited checkout defaults.
    if !development_mode || explicit_project_root.is_some_and(|value| !value.trim().is_empty()) {
        return None;
    }
    let project_root = source_seed
        .ancestors()
        .take(9)
        .find(|path| is_source_checkout(path))?
        .canonicalize()
        .ok()?;
    let mut paths = Vec::new();
    for (variable, raw, default_name) in [
        ("AUTOCARTO_HOME", runtime_home, "autocarto_data"),
        ("AUTOCARTO_ASSETS_ROOT", assets_root, "autocarto"),
    ] {
        let Some(raw) = raw else { continue };
        let path = Path::new(raw.trim());
        if !path.is_absolute() || path.file_name() != Some(std::ffi::OsStr::new(default_name)) {
            continue;
        }
        let Some(other_root) = path.parent().and_then(|parent| parent.canonicalize().ok()) else {
            continue;
        };
        if other_root != project_root && is_source_checkout(&other_root) {
            paths.push(PathCorrection {
                variable,
                previous: path.to_path_buf(),
                replacement: project_root.join(default_name),
            });
        }
    }
    if paths.is_empty() {
        None
    } else {
        Some(DevelopmentPathCorrections {
            project_root,
            paths,
        })
    }
}

fn is_source_checkout(root: &Path) -> bool {
    root.join("autocarto/skills/system").is_dir()
        && root.join("carto_agent/go.mod").is_file()
        && root.join("autocarto-web/src-tauri/Cargo.toml").is_file()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::time::{SystemTime, UNIX_EPOCH};

    struct Checkouts {
        root: PathBuf,
        current: PathBuf,
        other: PathBuf,
    }

    impl Checkouts {
        fn new() -> Self {
            let nonce = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos();
            let root = std::env::temp_dir().join(format!("autocarto-checkout-paths-{nonce}"));
            let current = root.join("current");
            let other = root.join("other");
            for checkout in [&current, &other] {
                fs::create_dir_all(checkout.join("autocarto/skills/system")).unwrap();
                fs::create_dir_all(checkout.join("autocarto-web/src-tauri")).unwrap();
                fs::create_dir_all(checkout.join("carto_agent")).unwrap();
                fs::create_dir_all(checkout.join("autocarto_data")).unwrap();
                fs::write(checkout.join("carto_agent/go.mod"), "module fixture").unwrap();
                fs::write(checkout.join("autocarto-web/src-tauri/Cargo.toml"), "").unwrap();
            }
            Self {
                root,
                current,
                other,
            }
        }

        fn corrections(
            &self,
            home: Option<&Path>,
            assets: Option<&Path>,
        ) -> Option<DevelopmentPathCorrections> {
            inherited_checkout_path_corrections(
                true,
                &self.current.join("autocarto-web/src-tauri"),
                None,
                home.and_then(Path::to_str),
                assets.and_then(Path::to_str),
            )
        }
    }

    impl Drop for Checkouts {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.root);
        }
    }

    #[test]
    fn corrects_inherited_default_paths_from_another_checkout() {
        let c = Checkouts::new();
        let home = c.other.join("autocarto_data");
        let assets = c.other.join("autocarto");
        let corrections = c
            .corrections(Some(&home), Some(&assets))
            .expect("foreign defaults must not bind this checkout");
        let root = c.current.canonicalize().unwrap();
        assert_eq!(corrections.project_root, root);
        assert_eq!(
            corrections.paths,
            vec![
                PathCorrection {
                    variable: "AUTOCARTO_HOME",
                    previous: home,
                    replacement: root.join("autocarto_data")
                },
                PathCorrection {
                    variable: "AUTOCARTO_ASSETS_ROOT",
                    previous: assets,
                    replacement: root.join("autocarto")
                },
            ]
        );
    }

    #[test]
    fn corrects_each_inherited_default_without_requiring_the_other() {
        let c = Checkouts::new();
        for (home, assets, variable) in [
            (Some(c.other.join("autocarto_data")), None, "AUTOCARTO_HOME"),
            (
                None,
                Some(c.other.join("autocarto")),
                "AUTOCARTO_ASSETS_ROOT",
            ),
        ] {
            let corrections = c.corrections(home.as_deref(), assets.as_deref()).unwrap();
            assert_eq!(corrections.paths.len(), 1);
            assert_eq!(corrections.paths[0].variable, variable);
        }
    }

    #[test]
    fn preserves_the_other_custom_override_while_correcting_a_default() {
        let c = Checkouts::new();
        let custom_home = c.root.join("shared-runtime");
        let custom_assets = c.root.join("shared-assets");
        fs::create_dir_all(custom_assets.join("skills/system")).unwrap();
        let assets_only = c
            .corrections(Some(&custom_home), Some(&c.other.join("autocarto")))
            .unwrap();
        assert_eq!(assets_only.paths.len(), 1);
        assert_eq!(assets_only.paths[0].variable, "AUTOCARTO_ASSETS_ROOT");
        let home_only = c
            .corrections(Some(&c.other.join("autocarto_data")), Some(&custom_assets))
            .unwrap();
        assert_eq!(home_only.paths.len(), 1);
        assert_eq!(home_only.paths[0].variable, "AUTOCARTO_HOME");
    }

    #[test]
    fn preserves_explicit_project_roots_and_release_configuration() {
        let c = Checkouts::new();
        let home = c.other.join("autocarto_data");
        let assets = c.other.join("autocarto");
        for (development_mode, explicit_root) in [
            (false, None),
            (true, c.current.to_str()),
            (true, c.other.to_str()),
        ] {
            assert!(inherited_checkout_path_corrections(
                development_mode,
                &c.current.join("autocarto-web/src-tauri"),
                explicit_root,
                home.to_str(),
                assets.to_str(),
            )
            .is_none());
        }
    }

    #[test]
    fn preserves_current_checkout_custom_absolute_and_relative_paths() {
        let c = Checkouts::new();
        let external = c.root.join("external");
        fs::create_dir_all(external.join("autocarto/skills/system")).unwrap();
        for (home, assets) in [
            (
                c.current.join("autocarto_data"),
                c.current.join("autocarto"),
            ),
            (
                c.other.join("custom-runtime"),
                c.other.join("custom-assets"),
            ),
            (external.join("autocarto_data"), external.join("autocarto")),
            (PathBuf::from("custom/runtime"), PathBuf::from("autocarto")),
        ] {
            assert!(c.corrections(Some(&home), Some(&assets)).is_none());
        }
    }

    #[test]
    fn does_not_rebind_without_a_verified_current_source_checkout() {
        let c = Checkouts::new();
        assert!(inherited_checkout_path_corrections(
            true,
            &c.root.join("unknown"),
            None,
            c.other.join("autocarto_data").to_str(),
            c.other.join("autocarto").to_str(),
        )
        .is_none());
    }
}
