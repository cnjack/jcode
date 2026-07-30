//! Native application discovery for the desktop "Open in" menu.
//!
//! Each platform owns its discovery and launch details:
//! - macOS resolves bundle identifiers through Launch Services.
//! - Windows validates installed applications through the uninstall registry
//!   and well-known executable locations.
//! - Linux reads freedesktop `.desktop` entries and icon themes.
//!
//! The webview only receives opaque application IDs and can never provide an
//! executable or command line. Workspace paths are canonicalized and checked
//! again in this native boundary before an application is started.

use serde::Serialize;
use std::path::{Path, PathBuf};

#[cfg(target_os = "linux")]
#[path = "desktop_apps/linux.rs"]
mod imp;
#[cfg(target_os = "macos")]
#[path = "desktop_apps/macos.rs"]
mod imp;
#[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
#[path = "desktop_apps/unsupported.rs"]
mod imp;
#[cfg(target_os = "windows")]
#[path = "desktop_apps/windows.rs"]
mod imp;

#[derive(Clone, Copy, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub(super) enum WorkspaceApplicationKind {
    Editor,
    FileManager,
    Terminal,
}

#[derive(Clone, Copy, Debug)]
pub(super) struct WorkspaceAppCandidate {
    pub id: &'static str,
    pub label: &'static str,
    pub group: &'static str,
    pub kind: WorkspaceApplicationKind,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceApplication {
    id: &'static str,
    label: &'static str,
    group: &'static str,
    kind: WorkspaceApplicationKind,
    icon_data_url: Option<String>,
}

impl WorkspaceApplication {
    pub(super) fn new(candidate: WorkspaceAppCandidate, icon_data_url: Option<String>) -> Self {
        Self {
            id: candidate.id,
            label: candidate.label,
            group: candidate.group,
            kind: candidate.kind,
            icon_data_url,
        }
    }
}

#[tauri::command]
pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
    imp::list_workspace_applications()
}

#[tauri::command]
pub fn open_workspace_in_application(path: String, app_id: String) -> Result<(), String> {
    let workspace = validate_workspace_path(&path)?;
    imp::open_workspace(&workspace, &app_id)
}

fn validate_workspace_path(path: &str) -> Result<PathBuf, String> {
    let requested = PathBuf::from(path);
    if !requested.is_absolute() {
        return Err("workspace path must be absolute".to_string());
    }

    let canonical = requested
        .canonicalize()
        .map_err(|error| format!("workspace path is unavailable: {error}"))?;
    if !canonical.is_dir() {
        return Err("workspace path must be a directory".to_string());
    }

    let home = local_home().ok_or_else(|| "the local home directory is unavailable".to_string())?;
    let canonical_home = home.canonicalize().unwrap_or(home);
    if !is_allowed_workspace_root(&canonical, &canonical_home) {
        return Err("workspace path is outside the allowed local roots".to_string());
    }

    Ok(canonical)
}

fn local_home() -> Option<PathBuf> {
    #[cfg(target_os = "windows")]
    {
        std::env::var_os("USERPROFILE")
            .or_else(|| std::env::var_os("HOME"))
            .map(PathBuf::from)
    }
    #[cfg(not(target_os = "windows"))]
    {
        std::env::var_os("HOME").map(PathBuf::from)
    }
}

fn is_allowed_workspace_root(path: &Path, home: &Path) -> bool {
    if path.starts_with(home) {
        return true;
    }

    #[cfg(target_os = "macos")]
    {
        path.starts_with("/Volumes")
    }
    #[cfg(target_os = "linux")]
    {
        ["/media", "/mnt", "/run/media", "/workspace", "/workspaces"]
            .iter()
            .any(|root| path.starts_with(root))
    }
    #[cfg(target_os = "windows")]
    {
        // Developer repositories commonly live on a non-system drive. Keep
        // network shares out of scope, but permit canonical drive-rooted paths.
        let value = path.to_string_lossy();
        path.has_root() && !value.starts_with(r"\\")
    }
    #[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
    {
        false
    }
}

#[cfg(all(test, any(target_os = "linux", target_os = "macos")))]
mod tests {
    use super::is_allowed_workspace_root;
    use std::path::Path;

    #[cfg(target_os = "macos")]
    #[test]
    fn macos_workspace_roots_are_scoped_to_home_and_volumes() {
        let home = Path::new("/Users/tester");
        assert!(is_allowed_workspace_root(
            Path::new("/Users/tester/work/jcode"),
            home
        ));
        assert!(is_allowed_workspace_root(
            Path::new("/Volumes/Workspace/jcode"),
            home
        ));
        assert!(!is_allowed_workspace_root(Path::new("/tmp/jcode"), home));
        assert!(!is_allowed_workspace_root(
            Path::new("/Users/another/jcode"),
            home
        ));
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn linux_workspace_roots_include_common_mount_locations() {
        let home = Path::new("/home/tester");
        assert!(is_allowed_workspace_root(
            Path::new("/home/tester/work/jcode"),
            home
        ));
        assert!(is_allowed_workspace_root(
            Path::new("/mnt/source/jcode"),
            home
        ));
        assert!(is_allowed_workspace_root(
            Path::new("/workspaces/jcode"),
            home
        ));
        assert!(!is_allowed_workspace_root(Path::new("/etc/jcode"), home));
    }
}
