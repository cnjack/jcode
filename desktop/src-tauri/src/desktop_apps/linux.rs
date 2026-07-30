use super::{WorkspaceAppCandidate, WorkspaceApplication, WorkspaceApplicationKind};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use std::collections::HashSet;
use std::ffi::OsStr;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

#[derive(Clone, Copy)]
struct LinuxCandidate {
    app: WorkspaceAppCandidate,
    desktop_ids: &'static [&'static str],
    executable_names: &'static [&'static str],
}

#[derive(Clone, Debug)]
struct DesktopEntry {
    path: PathBuf,
    name: String,
    icon: Option<String>,
    exec: String,
    try_exec: Option<String>,
}

#[derive(Clone)]
struct ResolvedApplication {
    entry: Option<DesktopEntry>,
    executable: Option<PathBuf>,
}

const APPLICATIONS: &[LinuxCandidate] = &[
    candidate(
        "vscode",
        "VS Code",
        "editor",
        WorkspaceApplicationKind::Editor,
        &[
            "code.desktop",
            "visual-studio-code.desktop",
            "com.visualstudio.code.desktop",
        ],
        &["code"],
    ),
    candidate(
        "cursor",
        "Cursor",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["cursor.desktop", "com.todesktop.230313mzl4w4u92.desktop"],
        &["cursor"],
    ),
    candidate(
        "zed",
        "Zed",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["dev.zed.zed.desktop", "zed.desktop", "zed-editor.desktop"],
        &["zed", "zeditor", "zed-editor"],
    ),
    candidate(
        "antigravity",
        "Antigravity",
        "editor",
        WorkspaceApplicationKind::Editor,
        &["antigravity.desktop", "com.google.antigravity.desktop"],
        &["antigravity"],
    ),
    candidate(
        "goland",
        "GoLand",
        "editor",
        WorkspaceApplicationKind::Editor,
        &[
            "goland.desktop",
            "jetbrains-goland.desktop",
            "com.jetbrains.goland.desktop",
        ],
        &["goland"],
    ),
    candidate(
        "nautilus",
        "Files",
        "system",
        WorkspaceApplicationKind::FileManager,
        &["org.gnome.nautilus.desktop", "nautilus.desktop"],
        &["nautilus"],
    ),
    candidate(
        "dolphin",
        "Dolphin",
        "system",
        WorkspaceApplicationKind::FileManager,
        &["org.kde.dolphin.desktop"],
        &["dolphin"],
    ),
    candidate(
        "thunar",
        "Thunar",
        "system",
        WorkspaceApplicationKind::FileManager,
        &["thunar.desktop"],
        &["thunar"],
    ),
    candidate(
        "nemo",
        "Nemo",
        "system",
        WorkspaceApplicationKind::FileManager,
        &["nemo.desktop"],
        &["nemo"],
    ),
    candidate(
        "ptyxis",
        "Ptyxis",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["org.gnome.ptyxis.desktop"],
        &["ptyxis"],
    ),
    candidate(
        "gnome-terminal",
        "Terminal",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["org.gnome.terminal.desktop", "gnome-terminal.desktop"],
        &["gnome-terminal"],
    ),
    candidate(
        "konsole",
        "Konsole",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["org.kde.konsole.desktop"],
        &["konsole"],
    ),
    candidate(
        "xfce-terminal",
        "Xfce Terminal",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["xfce4-terminal.desktop"],
        &["xfce4-terminal"],
    ),
    candidate(
        "ghostty",
        "Ghostty",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["com.mitchellh.ghostty.desktop"],
        &["ghostty"],
    ),
    candidate(
        "warp",
        "Warp",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["dev.warp.warp.desktop", "warp-terminal.desktop"],
        &["warp-terminal", "warp"],
    ),
    candidate(
        "alacritty",
        "Alacritty",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["alacritty.desktop", "org.alacritty.alacritty.desktop"],
        &["alacritty"],
    ),
    candidate(
        "kitty",
        "kitty",
        "system",
        WorkspaceApplicationKind::Terminal,
        &["kitty.desktop"],
        &["kitty"],
    ),
];

const fn candidate(
    id: &'static str,
    label: &'static str,
    group: &'static str,
    kind: WorkspaceApplicationKind,
    desktop_ids: &'static [&'static str],
    executable_names: &'static [&'static str],
) -> LinuxCandidate {
    LinuxCandidate {
        app: WorkspaceAppCandidate {
            id,
            label,
            group,
            kind,
        },
        desktop_ids,
        executable_names,
    }
}

pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
    let entries = read_desktop_entries();
    APPLICATIONS
        .iter()
        .filter_map(|candidate| {
            let resolved = resolve_application(candidate, &entries)?;
            let icon = resolved
                .entry
                .as_ref()
                .and_then(|entry| entry.icon.as_deref())
                .and_then(icon_data_url);
            Some(WorkspaceApplication::new(candidate.app, icon))
        })
        .collect()
}

pub fn open_workspace(workspace_path: &Path, app_id: &str) -> Result<(), String> {
    let candidate = APPLICATIONS
        .iter()
        .find(|candidate| candidate.app.id == app_id)
        .ok_or_else(|| format!("unsupported workspace application: {app_id}"))?;
    let entries = read_desktop_entries();
    let resolved = resolve_application(candidate, &entries)
        .ok_or_else(|| format!("{} is not installed", candidate.app.label))?;

    match candidate.app.kind {
        WorkspaceApplicationKind::FileManager => {
            if let Some(entry) = resolved.entry.as_ref() {
                if launch_desktop_entry(entry, workspace_path, candidate).is_ok() {
                    return Ok(());
                }
            }
            launch_executable(&resolved, workspace_path, candidate, true)
        }
        WorkspaceApplicationKind::Editor => {
            if let Some(entry) = resolved.entry.as_ref() {
                if launch_desktop_entry(entry, workspace_path, candidate).is_ok() {
                    return Ok(());
                }
            }
            launch_executable(&resolved, workspace_path, candidate, true)
        }
        WorkspaceApplicationKind::Terminal => {
            launch_executable(&resolved, workspace_path, candidate, false)
        }
    }
}

fn resolve_application(
    candidate: &LinuxCandidate,
    entries: &[DesktopEntry],
) -> Option<ResolvedApplication> {
    let entry = candidate
        .desktop_ids
        .iter()
        .find_map(|expected| {
            entries.iter().find(|entry| {
                desktop_entry_id(entry).eq_ignore_ascii_case(expected)
                    && desktop_entry_available(entry)
            })
        })
        .or_else(|| {
            entries.iter().find(|entry| {
                !desktop_entry_id(entry).contains("url-handler")
                    && desktop_executable_matches(entry, candidate)
                    && desktop_entry_available(entry)
            })
        })
        .cloned();
    let executable = entry
        .as_ref()
        .and_then(desktop_entry_executable)
        .or_else(|| find_named_executable(candidate.executable_names));

    if entry.is_some() || executable.is_some() {
        Some(ResolvedApplication { entry, executable })
    } else {
        None
    }
}

fn desktop_entry_id(entry: &DesktopEntry) -> String {
    entry
        .path
        .file_name()
        .and_then(OsStr::to_str)
        .unwrap_or_default()
        .to_ascii_lowercase()
}

fn desktop_executable_matches(entry: &DesktopEntry, candidate: &LinuxCandidate) -> bool {
    let executable = desktop_entry_command(entry)
        .and_then(|command| command.file_name().map(OsStr::to_owned))
        .and_then(|name| name.to_str().map(str::to_ascii_lowercase));
    executable.is_some_and(|name| {
        candidate
            .executable_names
            .iter()
            .any(|expected| name == expected.to_ascii_lowercase())
    })
}

fn desktop_entry_available(entry: &DesktopEntry) -> bool {
    entry
        .try_exec
        .as_deref()
        .map(resolve_executable)
        .map(|path| path.is_some())
        .unwrap_or(true)
}

fn desktop_entry_executable(entry: &DesktopEntry) -> Option<PathBuf> {
    entry
        .try_exec
        .as_deref()
        .and_then(resolve_executable)
        .or_else(|| desktop_entry_command(entry))
}

fn desktop_entry_command(entry: &DesktopEntry) -> Option<PathBuf> {
    let words = shlex::split(&entry.exec)?;
    let command = words
        .iter()
        .skip_while(|word| word.as_str() == "env" || is_environment_assignment(word))
        .find(|word| !word.starts_with('%'))?;
    resolve_executable(command)
}

fn is_environment_assignment(word: &str) -> bool {
    let Some((name, _)) = word.split_once('=') else {
        return false;
    };
    !name.is_empty()
        && name
            .bytes()
            .all(|byte| byte == b'_' || byte.is_ascii_alphanumeric())
}

fn launch_desktop_entry(
    entry: &DesktopEntry,
    workspace_path: &Path,
    candidate: &LinuxCandidate,
) -> Result<(), String> {
    let gio = resolve_executable("gio").ok_or_else(|| "gio is unavailable".to_string())?;
    spawn_detached(
        Command::new(gio)
            .arg("launch")
            .arg(&entry.path)
            .arg(workspace_path),
        candidate.app.label,
    )
}

fn launch_executable(
    resolved: &ResolvedApplication,
    workspace_path: &Path,
    candidate: &LinuxCandidate,
    pass_workspace: bool,
) -> Result<(), String> {
    let (executable, mut arguments, includes_workspace) =
        if let Some(entry) = resolved.entry.as_ref() {
            desktop_command(entry, workspace_path, pass_workspace)?
        } else {
            let executable = resolved
                .executable
                .clone()
                .ok_or_else(|| format!("{} executable is unavailable", candidate.app.label))?;
            (executable, Vec::new(), false)
        };

    if pass_workspace && !includes_workspace {
        arguments.push(workspace_path.as_os_str().to_owned());
    }

    let mut command = Command::new(executable);
    command.args(arguments).current_dir(workspace_path);
    spawn_detached(&mut command, candidate.app.label)
}

fn desktop_command(
    entry: &DesktopEntry,
    workspace_path: &Path,
    pass_workspace: bool,
) -> Result<(PathBuf, Vec<std::ffi::OsString>, bool), String> {
    let words = shlex::split(&entry.exec)
        .ok_or_else(|| format!("invalid desktop command for {}", entry.name))?;
    let mut words = words.into_iter();
    let command = words
        .next()
        .ok_or_else(|| format!("missing desktop command for {}", entry.name))?;
    let executable = resolve_executable(&command)
        .ok_or_else(|| format!("desktop executable is unavailable: {command}"))?;
    let workspace = workspace_path.to_string_lossy();
    let mut includes_workspace = false;
    let mut arguments = Vec::new();

    for word in words {
        if matches!(word.as_str(), "%i" | "%c" | "%k") {
            continue;
        }
        if matches!(word.as_str(), "%f" | "%F" | "%u" | "%U") {
            if pass_workspace {
                arguments.push(workspace_path.as_os_str().to_owned());
                includes_workspace = true;
            }
            continue;
        }

        let mut expanded = word.replace("%%", "%");
        for field in ["%f", "%F", "%u", "%U"] {
            if expanded.contains(field) {
                if pass_workspace {
                    expanded = expanded.replace(field, &workspace);
                    includes_workspace = true;
                } else {
                    expanded = expanded.replace(field, "");
                }
            }
        }
        if !expanded.is_empty() {
            arguments.push(expanded.into());
        }
    }

    Ok((executable, arguments, includes_workspace))
}

fn spawn_detached(command: &mut Command, label: &str) -> Result<(), String> {
    command
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|error| format!("could not start {label}: {error}"))
}

fn read_desktop_entries() -> Vec<DesktopEntry> {
    let mut entries = Vec::new();
    let mut seen = HashSet::new();
    for directory in application_directories() {
        let Ok(children) = fs::read_dir(directory) else {
            continue;
        };
        for child in children.flatten() {
            let path = child.path();
            if path.extension().and_then(OsStr::to_str) != Some("desktop") {
                continue;
            }
            let Some(id) = path
                .file_name()
                .and_then(OsStr::to_str)
                .map(str::to_ascii_lowercase)
            else {
                continue;
            };
            if !seen.insert(id) {
                continue;
            }
            if let Some(entry) = parse_desktop_entry(&path) {
                entries.push(entry);
            }
        }
    }
    entries
}

fn parse_desktop_entry(path: &Path) -> Option<DesktopEntry> {
    let source = fs::read_to_string(path).ok()?;
    let mut in_desktop_entry = false;
    let mut entry_type = String::new();
    let mut name = String::new();
    let mut icon = None;
    let mut exec = String::new();
    let mut try_exec = None;
    let mut hidden = false;

    for raw_line in source.lines() {
        let line = raw_line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if line.starts_with('[') && line.ends_with(']') {
            in_desktop_entry = line == "[Desktop Entry]";
            continue;
        }
        if !in_desktop_entry {
            continue;
        }
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        match key {
            "Type" => entry_type = value.trim().to_string(),
            "Name" => name = value.trim().to_string(),
            "Icon" => icon = non_empty(value),
            "Exec" => exec = value.trim().to_string(),
            "TryExec" => try_exec = non_empty(value),
            "Hidden" => hidden = value.trim().eq_ignore_ascii_case("true"),
            _ => {}
        }
    }

    if hidden || entry_type != "Application" || name.is_empty() || exec.is_empty() {
        return None;
    }
    Some(DesktopEntry {
        path: path.to_path_buf(),
        name,
        icon,
        exec,
        try_exec,
    })
}

fn non_empty(value: &str) -> Option<String> {
    let value = value.trim();
    (!value.is_empty()).then(|| value.to_string())
}

fn application_directories() -> Vec<PathBuf> {
    let mut directories = Vec::new();
    let home = std::env::var_os("HOME").map(PathBuf::from);
    if let Some(data_home) = std::env::var_os("XDG_DATA_HOME") {
        directories.push(PathBuf::from(data_home).join("applications"));
    } else if let Some(home) = home.as_ref() {
        directories.push(home.join(".local/share/applications"));
    }
    for directory in system_data_directories() {
        directories.push(directory.join("applications"));
    }
    directories.push(PathBuf::from("/var/lib/snapd/desktop/applications"));
    deduplicate_paths(directories)
}

fn system_data_directories() -> Vec<PathBuf> {
    let configured = std::env::var("XDG_DATA_DIRS")
        .unwrap_or_else(|_| "/usr/local/share:/usr/share".to_string());
    let mut directories: Vec<PathBuf> = configured
        .split(':')
        .filter(|value| !value.is_empty())
        .map(PathBuf::from)
        .collect();
    directories.push(PathBuf::from("/var/lib/flatpak/exports/share"));
    if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
        directories.push(home.join(".local/share/flatpak/exports/share"));
    }
    deduplicate_paths(directories)
}

fn deduplicate_paths(paths: Vec<PathBuf>) -> Vec<PathBuf> {
    let mut seen = HashSet::new();
    paths
        .into_iter()
        .filter(|path| seen.insert(path.clone()))
        .collect()
}

fn find_named_executable(names: &[&str]) -> Option<PathBuf> {
    names.iter().find_map(|name| resolve_executable(name))
}

fn resolve_executable(value: &str) -> Option<PathBuf> {
    let expanded = if let Some(relative) = value.strip_prefix("~/") {
        std::env::var_os("HOME").map(PathBuf::from)?.join(relative)
    } else {
        PathBuf::from(value)
    };
    if expanded.components().count() > 1 {
        return executable_file(&expanded).then_some(expanded);
    }

    executable_search_directories()
        .into_iter()
        .map(|directory| directory.join(value))
        .find(|path| executable_file(path))
}

fn executable_search_directories() -> Vec<PathBuf> {
    let mut paths: Vec<PathBuf> = std::env::var_os("PATH")
        .map(|value| std::env::split_paths(&value).collect())
        .unwrap_or_default();
    paths.extend([
        PathBuf::from("/usr/local/bin"),
        PathBuf::from("/usr/bin"),
        PathBuf::from("/snap/bin"),
    ]);
    if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
        paths.push(home.join(".local/bin"));
        paths.push(home.join(".local/share/JetBrains/Toolbox/scripts"));
    }
    deduplicate_paths(paths)
}

fn executable_file(path: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;

    fs::metadata(path)
        .map(|metadata| metadata.is_file() && metadata.permissions().mode() & 0o111 != 0)
        .unwrap_or(false)
}

fn icon_data_url(icon: &str) -> Option<String> {
    let icon_path = resolve_icon(icon)?;
    let metadata = fs::metadata(&icon_path).ok()?;
    if !metadata.is_file() || metadata.len() > 4 * 1024 * 1024 {
        return None;
    }
    let mime = match icon_path
        .extension()
        .and_then(OsStr::to_str)?
        .to_ascii_lowercase()
        .as_str()
    {
        "png" => "image/png",
        "svg" => "image/svg+xml",
        "jpg" | "jpeg" => "image/jpeg",
        "webp" => "image/webp",
        _ => return None,
    };
    let bytes = fs::read(icon_path).ok()?;
    Some(format!("data:{mime};base64,{}", STANDARD.encode(bytes)))
}

fn resolve_icon(icon: &str) -> Option<PathBuf> {
    let direct = PathBuf::from(icon);
    if direct.is_absolute() && direct.is_file() {
        return Some(direct);
    }

    let icon_name = direct.file_stem().and_then(OsStr::to_str).unwrap_or(icon);
    let requested_extension = direct.extension().and_then(OsStr::to_str);
    let extensions = match requested_extension {
        Some(extension) => vec![extension],
        None => vec!["png", "svg", "webp"],
    };

    for data_dir in icon_data_directories() {
        for theme in ["hicolor", "Adwaita", "breeze", "HighContrast"] {
            for size in ["64x64", "48x48", "128x128", "256x256", "32x32", "scalable"] {
                for context in ["apps", "applications"] {
                    for extension in &extensions {
                        let path = data_dir
                            .join("icons")
                            .join(theme)
                            .join(size)
                            .join(context)
                            .join(format!("{icon_name}.{extension}"));
                        if path.is_file() {
                            return Some(path);
                        }
                    }
                }
            }
        }
        for extension in &extensions {
            let path = data_dir
                .join("pixmaps")
                .join(format!("{icon_name}.{extension}"));
            if path.is_file() {
                return Some(path);
            }
        }
    }
    None
}

fn icon_data_directories() -> Vec<PathBuf> {
    let mut directories = Vec::new();
    if let Some(data_home) = std::env::var_os("XDG_DATA_HOME") {
        directories.push(PathBuf::from(data_home));
    } else if let Some(home) = std::env::var_os("HOME").map(PathBuf::from) {
        directories.push(home.join(".local/share"));
        directories.push(home.join(".icons"));
    }
    directories.extend(system_data_directories());
    deduplicate_paths(directories)
}

#[cfg(test)]
mod tests {
    use super::{desktop_command, parse_desktop_entry, resolve_application, DesktopEntry};
    use std::fs;

    #[test]
    fn parses_a_visible_application_entry() {
        let directory = tempfile::tempdir().expect("temp directory");
        let path = directory.path().join("code.desktop");
        fs::write(
            &path,
            "[Desktop Entry]\nType=Application\nName=Code\nExec=code %F\nIcon=code\n",
        )
        .expect("desktop entry");
        let entry = parse_desktop_entry(&path).expect("valid entry");
        assert_eq!(entry.name, "Code");
        assert_eq!(entry.icon.as_deref(), Some("code"));
        assert_eq!(entry.exec, "code %F");
    }

    #[test]
    fn ignores_hidden_desktop_entries() {
        let directory = tempfile::tempdir().expect("temp directory");
        let path = directory.path().join("hidden.desktop");
        fs::write(
            &path,
            "[Desktop Entry]\nType=Application\nName=Hidden\nExec=hidden\nHidden=true\n",
        )
        .expect("desktop entry");
        assert!(parse_desktop_entry(&path).is_none());
    }

    #[test]
    fn desktop_field_code_receives_the_workspace_once() {
        let directory = tempfile::tempdir().expect("temp directory");
        let executable = directory.path().join("editor");
        fs::write(&executable, "#!/bin/sh\n").expect("executable");
        let mut permissions = fs::metadata(&executable).expect("metadata").permissions();
        use std::os::unix::fs::PermissionsExt;
        permissions.set_mode(0o755);
        fs::set_permissions(&executable, permissions).expect("permissions");
        let entry = super::DesktopEntry {
            path: directory.path().join("editor.desktop"),
            name: "Editor".to_string(),
            icon: None,
            exec: format!("{} --new-window %F", executable.display()),
            try_exec: None,
        };
        let workspace = directory.path().join("workspace");
        let (_, arguments, included) =
            desktop_command(&entry, &workspace, true).expect("desktop command");
        assert!(included);
        assert_eq!(
            arguments
                .iter()
                .filter(|argument| argument.as_os_str() == workspace.as_os_str())
                .count(),
            1
        );
    }

    #[test]
    fn prefers_the_primary_desktop_entry_over_a_url_handler() {
        let directory = tempfile::tempdir().expect("temp directory");
        let executable = directory.path().join("cursor");
        fs::write(&executable, "#!/bin/sh\n").expect("executable");
        let mut permissions = fs::metadata(&executable).expect("metadata").permissions();
        use std::os::unix::fs::PermissionsExt;
        permissions.set_mode(0o755);
        fs::set_permissions(&executable, permissions).expect("permissions");
        let entries = vec![
            DesktopEntry {
                path: directory.path().join("cursor-url-handler.desktop"),
                name: "Cursor URL Handler".to_string(),
                icon: None,
                exec: format!("{} --open-url %U", executable.display()),
                try_exec: None,
            },
            DesktopEntry {
                path: directory.path().join("cursor.desktop"),
                name: "Cursor".to_string(),
                icon: None,
                exec: format!("{} %F", executable.display()),
                try_exec: None,
            },
        ];
        let cursor = super::APPLICATIONS
            .iter()
            .find(|candidate| candidate.app.id == "cursor")
            .expect("Cursor candidate");
        let resolved = resolve_application(cursor, &entries).expect("resolved Cursor");
        assert_eq!(
            resolved
                .entry
                .expect("desktop entry")
                .path
                .file_name()
                .and_then(std::ffi::OsStr::to_str),
            Some("cursor.desktop")
        );
    }
}
