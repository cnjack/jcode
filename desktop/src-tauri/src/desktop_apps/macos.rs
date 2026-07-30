use super::{WorkspaceAppCandidate, WorkspaceApplication, WorkspaceApplicationKind};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use icns::IconFamily;
use objc2_app_kit::NSWorkspace;
use objc2_foundation::{NSArray, NSString, NSURL};
use plist::Value;
use std::fs::File;
use std::io::BufReader;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

#[derive(Clone, Copy)]
struct MacOSCandidate {
    app: WorkspaceAppCandidate,
    bundle_ids: &'static [&'static str],
    reveal_in_finder: bool,
}

const APPLICATIONS: &[MacOSCandidate] = &[
    candidate(
        "vscode",
        "VS Code",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["com.microsoft.VSCode"],
        false,
    ),
    candidate(
        "cursor",
        "Cursor",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["com.todesktop.230313mzl4w4u92"],
        false,
    ),
    candidate(
        "zed",
        "Zed",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["dev.zed.Zed", "dev.zed.Zed-Preview"],
        false,
    ),
    candidate(
        "antigravity",
        "Antigravity",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["com.google.antigravity"],
        false,
    ),
    candidate(
        "xcode",
        "Xcode",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["com.apple.dt.Xcode"],
        false,
    ),
    candidate(
        "goland",
        "GoLand",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["com.jetbrains.goland"],
        false,
    ),
    candidate(
        "finder",
        "Finder",
        "system",
        WorkspaceApplicationKind::FileManager,
        &["com.apple.finder"],
        true,
    ),
    candidate(
        "terminal",
        "Terminal",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["com.apple.Terminal"],
        false,
    ),
    candidate(
        "iterm",
        "iTerm2",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["com.googlecode.iterm2"],
        false,
    ),
    candidate(
        "ghostty",
        "Ghostty",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["com.mitchellh.ghostty"],
        false,
    ),
    candidate(
        "warp",
        "Warp",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["dev.warp.Warp-Stable", "dev.warp.Warp"],
        false,
    ),
];

const fn candidate(
    id: &'static str,
    label: &'static str,
    group: &'static str,
    kind: WorkspaceApplicationKind,
    bundle_ids: &'static [&'static str],
    reveal_in_finder: bool,
) -> MacOSCandidate {
    MacOSCandidate {
        app: WorkspaceAppCandidate {
            id,
            label,
            group,
            kind,
        },
        bundle_ids,
        reveal_in_finder,
    }
}

pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
    APPLICATIONS
        .iter()
        .filter_map(|candidate| {
            let app_path = resolve_application(candidate)?;
            Some(WorkspaceApplication::new(
                candidate.app,
                app_icon_data_url(&app_path),
            ))
        })
        .collect()
}

pub fn open_workspace(workspace_path: &Path, app_id: &str) -> Result<(), String> {
    let candidate = APPLICATIONS
        .iter()
        .find(|candidate| candidate.app.id == app_id)
        .ok_or_else(|| format!("unsupported workspace application: {app_id}"))?;
    let app_path = resolve_application(candidate)
        .ok_or_else(|| format!("{} is not installed", candidate.app.label))?;

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
        .map_err(|error| format!("could not start {}: {error}", candidate.app.label))?;

    if output.status.success() {
        return Ok(());
    }

    let detail = String::from_utf8_lossy(&output.stderr).trim().to_string();
    if detail.is_empty() {
        Err(format!(
            "{} exited with status {}",
            candidate.app.label, output.status
        ))
    } else {
        Err(format!("could not open {}: {detail}", candidate.app.label))
    }
}

fn resolve_application(candidate: &MacOSCandidate) -> Option<PathBuf> {
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

#[cfg(test)]
mod tests {
    #[test]
    fn discovery_returns_finder_with_its_native_icon() {
        let applications = super::list_workspace_applications();
        let finder = applications
            .iter()
            .find(|application| application.id == "finder")
            .expect("Finder should be registered with Launch Services");
        assert!(finder
            .icon_data_url
            .as_deref()
            .is_some_and(|icon| icon.starts_with("data:image/png;base64,")));
    }

    #[test]
    fn application_ids_are_opaque() {
        let result = super::open_workspace(
            std::path::Path::new("/"),
            "../../Applications/Calculator.app",
        );
        assert!(result.is_err());
    }
}
