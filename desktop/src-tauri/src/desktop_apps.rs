//! Native macOS application discovery for the title-bar "Open in" menu.
//!
//! The frontend never guesses whether an editor is installed and never sends
//! an executable path. This module resolves a curated set of developer apps by
//! bundle identifier through Launch Services, extracts each installed app's
//! own icon, and accepts only the opaque IDs declared below when opening a
//! workspace.

use serde::Serialize;
use std::path::{Path, PathBuf};

#[derive(Clone, Copy)]
struct WorkspaceAppCandidate {
    id: &'static str,
    label: &'static str,
    bundle_ids: &'static [&'static str],
    group: &'static str,
    reveal_in_finder: bool,
}

const WORKSPACE_APP_CANDIDATES: &[WorkspaceAppCandidate] = &[
    WorkspaceAppCandidate {
        id: "vscode",
        label: "VS Code",
        bundle_ids: &["com.microsoft.VSCode"],
        group: "editor",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "cursor",
        label: "Cursor",
        bundle_ids: &["com.todesktop.230313mzl4w4u92"],
        group: "editor",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "zed",
        label: "Zed",
        bundle_ids: &["dev.zed.Zed", "dev.zed.Zed-Preview"],
        group: "editor",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "antigravity",
        label: "Antigravity",
        bundle_ids: &["com.google.antigravity"],
        group: "editor",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "finder",
        label: "Finder",
        bundle_ids: &["com.apple.finder"],
        group: "system",
        reveal_in_finder: true,
    },
    WorkspaceAppCandidate {
        id: "terminal",
        label: "Terminal",
        bundle_ids: &["com.apple.Terminal"],
        group: "system",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "iterm",
        label: "iTerm2",
        bundle_ids: &["com.googlecode.iterm2"],
        group: "system",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "ghostty",
        label: "Ghostty",
        bundle_ids: &["com.mitchellh.ghostty"],
        group: "system",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "warp",
        label: "Warp",
        bundle_ids: &["dev.warp.Warp-Stable", "dev.warp.Warp"],
        group: "system",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "xcode",
        label: "Xcode",
        bundle_ids: &["com.apple.dt.Xcode"],
        group: "system",
        reveal_in_finder: false,
    },
    WorkspaceAppCandidate {
        id: "goland",
        label: "GoLand",
        bundle_ids: &["com.jetbrains.goland"],
        group: "system",
        reveal_in_finder: false,
    },
];

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceApplication {
    id: &'static str,
    label: &'static str,
    group: &'static str,
    icon_data_url: Option<String>,
}

#[tauri::command]
pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
    imp::list_workspace_applications()
}

#[tauri::command]
pub fn open_workspace_in_application(path: String, app_id: String) -> Result<(), String> {
    let workspace = validate_workspace_path(&path)?;
    let candidate = candidate_by_id(&app_id)
        .ok_or_else(|| format!("unsupported workspace application: {app_id}"))?;
    imp::open_workspace(&workspace, candidate)
}

fn candidate_by_id(id: &str) -> Option<&'static WorkspaceAppCandidate> {
    WORKSPACE_APP_CANDIDATES
        .iter()
        .find(|candidate| candidate.id == id)
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

    let home = std::env::var_os("HOME")
        .map(PathBuf::from)
        .ok_or_else(|| "HOME is unavailable".to_string())?;
    let canonical_home = home.canonicalize().unwrap_or(home);
    if !is_allowed_workspace_root(&canonical, &canonical_home) {
        return Err("workspace path is outside the allowed local roots".to_string());
    }

    Ok(canonical)
}

fn is_allowed_workspace_root(path: &Path, home: &Path) -> bool {
    path.starts_with(home) || path.starts_with("/Volumes")
}

#[cfg(target_os = "macos")]
mod imp {
    use super::{WorkspaceAppCandidate, WorkspaceApplication, WORKSPACE_APP_CANDIDATES};
    use base64::{engine::general_purpose::STANDARD, Engine as _};
    use icns::IconFamily;
    use objc2_app_kit::NSWorkspace;
    use objc2_foundation::{NSArray, NSString, NSURL};
    use plist::Value;
    use std::fs::File;
    use std::io::BufReader;
    use std::path::{Path, PathBuf};
    use std::process::{Command, Stdio};

    pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
        WORKSPACE_APP_CANDIDATES
            .iter()
            .filter_map(|candidate| {
                let app_path = resolve_application(candidate)?;
                Some(WorkspaceApplication {
                    id: candidate.id,
                    label: candidate.label,
                    group: candidate.group,
                    icon_data_url: app_icon_data_url(&app_path),
                })
            })
            .collect()
    }

    pub fn open_workspace(
        workspace_path: &Path,
        candidate: &WorkspaceAppCandidate,
    ) -> Result<(), String> {
        let app_path = resolve_application(candidate)
            .ok_or_else(|| format!("{} is not installed", candidate.label))?;

        if candidate.reveal_in_finder {
            reveal_in_finder(workspace_path);
            return Ok(());
        }

        let output = Command::new("/usr/bin/open")
            .arg("-a")
            .arg(&app_path)
            .arg("--")
            .arg(workspace_path)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .output()
            .map_err(|error| format!("could not start {}: {error}", candidate.label))?;

        if output.status.success() {
            return Ok(());
        }

        let detail = String::from_utf8_lossy(&output.stderr).trim().to_string();
        if detail.is_empty() {
            Err(format!(
                "{} exited with status {}",
                candidate.label, output.status
            ))
        } else {
            Err(format!("could not open {}: {detail}", candidate.label))
        }
    }

    fn resolve_application(candidate: &WorkspaceAppCandidate) -> Option<PathBuf> {
        let workspace = NSWorkspace::sharedWorkspace();
        for bundle_id in candidate.bundle_ids {
            let identifier = NSString::from_str(bundle_id);
            let Some(url) = workspace.URLForApplicationWithBundleIdentifier(&identifier) else {
                continue;
            };
            let Some(path) = url.path() else {
                continue;
            };
            let app_path = PathBuf::from(path.to_string());
            if app_path.is_dir() {
                return Some(app_path);
            }
        }
        None
    }

    fn reveal_in_finder(path: &Path) {
        let workspace = NSWorkspace::sharedWorkspace();
        let path = NSString::from_str(&path.to_string_lossy());
        let url = NSURL::fileURLWithPath(&path);
        let urls = NSArray::from_retained_slice(&[url]);
        workspace.activateFileViewerSelectingURLs(&urls);
    }

    fn app_icon_data_url(app_path: &Path) -> Option<String> {
        let icon_path = app_icon_path(app_path)?;
        let file = BufReader::new(File::open(icon_path).ok()?);
        let family = IconFamily::read(file).ok()?;
        let mut icon_types = family.available_icons().to_vec();
        icon_types.sort_by_key(|icon_type| {
            let width = icon_type.pixel_width();
            if width >= 64 {
                width - 64
            } else {
                10_000 + 64 - width
            }
        });

        for icon_type in icon_types {
            let Ok(image) = family.get_icon_with_type(icon_type) else {
                continue;
            };
            let mut png = Vec::new();
            if image.write_png(&mut png).is_ok() {
                return Some(format!("data:image/png;base64,{}", STANDARD.encode(png)));
            }
        }
        None
    }

    fn app_icon_path(app_path: &Path) -> Option<PathBuf> {
        let info = Value::from_file(app_path.join("Contents/Info.plist")).ok()?;
        let dictionary = info.as_dictionary()?;
        let icon_name = dictionary
            .get("CFBundleIconFile")
            .and_then(Value::as_string)
            .or_else(|| {
                dictionary
                    .get("CFBundleIconName")
                    .and_then(Value::as_string)
            })?;

        let mut icon_path = app_path.join("Contents/Resources").join(icon_name);
        if icon_path.extension().is_none() {
            icon_path.set_extension("icns");
        }
        icon_path.is_file().then_some(icon_path)
    }
}

#[cfg(not(target_os = "macos"))]
mod imp {
    use super::{WorkspaceAppCandidate, WorkspaceApplication};
    use std::path::Path;

    pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
        Vec::new()
    }

    pub fn open_workspace(
        _workspace_path: &Path,
        _candidate: &WorkspaceAppCandidate,
    ) -> Result<(), String> {
        Err("workspace applications are only available on macOS".to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::{candidate_by_id, is_allowed_workspace_root};
    use std::path::Path;

    #[test]
    fn candidate_lookup_accepts_only_declared_ids() {
        assert_eq!(
            candidate_by_id("vscode").map(|app| app.label),
            Some("VS Code")
        );
        assert!(candidate_by_id("../../Applications/Calculator.app").is_none());
    }

    #[test]
    fn workspace_roots_are_scoped_to_home_and_volumes() {
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

    #[cfg(target_os = "macos")]
    #[test]
    fn macos_discovery_returns_finder_with_its_native_icon() {
        let applications = super::imp::list_workspace_applications();
        let finder = applications
            .iter()
            .find(|application| application.id == "finder")
            .expect("Finder should be registered with Launch Services");
        assert!(finder
            .icon_data_url
            .as_deref()
            .is_some_and(|icon| icon.starts_with("data:image/png;base64,")));
    }
}
