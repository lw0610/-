fn main() {
    let manifest = std::path::PathBuf::from(std::env::var_os("CARGO_MANIFEST_DIR").unwrap());
    let source = manifest.join("../../carto_agent");
    // Watch source directories too, so newly added Go files and embedded assets
    // trigger a rebuild. Avoid runtime data/log/output folders at the module root.
    for entry in ["go.mod", "go.sum", "cmd", "internal", "defaults"] {
        println!("cargo:rerun-if-changed={}", source.join(entry).display());
    }
    let script = manifest.join("../scripts/build-agent.mjs");
    println!("cargo:rerun-if-changed={}", script.display());
    for key in ["GOTOOLCHAIN", "CGO_ENABLED", "CC", "GOFLAGS"] {
        println!("cargo:rerun-if-env-changed={key}");
    }
    let target = std::env::var("TARGET").expect("Cargo must supply TARGET for the sidecar build");
    println!("cargo:rerun-if-env-changed=CC_{}", target.replace('-', "_"));
    // beforeDevCommand runs concurrently with Cargo and an existing frontend
    // server can already be ready. Build synchronously at the actual copy point.
    let status = std::process::Command::new("node")
        .arg(&script)
        .arg("--target")
        .arg(&target)
        .status()
        .expect("Node.js is required to build the current Go sidecar");
    assert!(
        status.success(),
        "Go sidecar build failed; refusing to copy a stale backend"
    );
    tauri_build::build()
}
