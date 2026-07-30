use super::{WorkspaceAppCandidate, WorkspaceApplication, WorkspaceApplicationKind};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use std::ffi::OsStr;
use std::mem;
use std::os::windows::ffi::OsStrExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::ptr;
use windows_sys::Win32::Graphics::Gdi::{
    DeleteObject, GetDC, GetDIBits, GetObjectW, ReleaseDC, BITMAP, BITMAPINFO, BITMAPINFOHEADER,
    BI_RGB, DIB_RGB_COLORS, HBITMAP, HDC,
};
use windows_sys::Win32::UI::Shell::ExtractIconExW;
use windows_sys::Win32::UI::WindowsAndMessaging::{DestroyIcon, GetIconInfo, HICON, ICONINFO};
use winreg::enums::{
    HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE, KEY_READ, KEY_WOW64_32KEY, KEY_WOW64_64KEY,
};
use winreg::RegKey;

#[derive(Clone, Copy)]
struct WindowsCandidate {
    app: WorkspaceAppCandidate,
    display_name_prefixes: &'static [&'static str],
    publishers: &'static [&'static str],
    executable_names: &'static [&'static str],
    install_relative_paths: &'static [&'static str],
}

#[derive(Debug)]
struct RegistryApplication {
    display_name: String,
    publisher: String,
    display_icon: Option<PathBuf>,
    install_location: Option<PathBuf>,
}

const APPLICATIONS: &[WindowsCandidate] = &[
    candidate(
        "vscode",
        "VS Code",
        WorkspaceApplicationKind::Editor,
        &["Microsoft Visual Studio Code"],
        &["Microsoft Corporation"],
        &["Code.exe"],
        &["Code.exe"],
    ),
    candidate(
        "cursor",
        "Cursor",
        WorkspaceApplicationKind::Editor,
        &["Cursor", "Cursor (User)"],
        &["Anysphere", "Cursor AI"],
        &["Cursor.exe"],
        &["Cursor.exe"],
    ),
    candidate(
        "zed",
        "Zed",
        WorkspaceApplicationKind::Editor,
        &["Zed"],
        &["Zed Industries"],
        &["Zed.exe"],
        &["Zed.exe"],
    ),
    candidate(
        "antigravity",
        "Antigravity",
        WorkspaceApplicationKind::Editor,
        &["Antigravity"],
        &["Google"],
        &["Antigravity.exe", "antigravity.exe"],
        &["Antigravity.exe", "antigravity.exe"],
    ),
    candidate(
        "goland",
        "GoLand",
        WorkspaceApplicationKind::Editor,
        &["GoLand", "JetBrains GoLand", "JetBrains Toolbox (GoLand"],
        &["JetBrains"],
        &["goland64.exe", "goland.exe"],
        &["bin\\goland64.exe", "bin\\goland.exe"],
    ),
    candidate(
        "explorer",
        "File Explorer",
        WorkspaceApplicationKind::FileManager,
        &[],
        &[],
        &["explorer.exe"],
        &[],
    ),
    candidate(
        "windows-terminal",
        "Windows Terminal",
        WorkspaceApplicationKind::Terminal,
        &["Windows Terminal"],
        &["Microsoft Corporation"],
        &["wt.exe"],
        &["wt.exe"],
    ),
    candidate(
        "ghostty",
        "Ghostty",
        WorkspaceApplicationKind::Terminal,
        &["Ghostty"],
        &[],
        &["ghostty.exe"],
        &["ghostty.exe", "bin\\ghostty.exe"],
    ),
    candidate(
        "warp",
        "Warp",
        WorkspaceApplicationKind::Terminal,
        &["Warp"],
        &[],
        &["Warp.exe", "warp.exe"],
        &["Warp.exe", "warp.exe"],
    ),
];

const fn candidate(
    id: &'static str,
    label: &'static str,
    kind: WorkspaceApplicationKind,
    display_name_prefixes: &'static [&'static str],
    publishers: &'static [&'static str],
    executable_names: &'static [&'static str],
    install_relative_paths: &'static [&'static str],
) -> WindowsCandidate {
    let group = match kind {
        WorkspaceApplicationKind::Editor => "editor",
        WorkspaceApplicationKind::FileManager | WorkspaceApplicationKind::Terminal => "system",
    };
    WindowsCandidate {
        app: WorkspaceAppCandidate {
            id,
            label,
            group,
            kind,
        },
        display_name_prefixes,
        publishers,
        executable_names,
        install_relative_paths,
    }
}

pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
    let registry = installed_registry_applications();
    APPLICATIONS
        .iter()
        .filter_map(|candidate| {
            let executable = resolve_application(candidate, &registry)?;
            let icon = app_icon_data_url(&executable);
            Some(WorkspaceApplication::new(candidate.app, icon))
        })
        .collect()
}

pub fn open_workspace(workspace_path: &Path, app_id: &str) -> Result<(), String> {
    let candidate = APPLICATIONS
        .iter()
        .find(|candidate| candidate.app.id == app_id)
        .ok_or_else(|| format!("unsupported workspace application: {app_id}"))?;
    let registry = installed_registry_applications();
    let executable = resolve_application(candidate, &registry)
        .ok_or_else(|| format!("{} is not installed", candidate.app.label))?;
    let mut command = Command::new(executable);

    match candidate.app.kind {
        WorkspaceApplicationKind::Editor | WorkspaceApplicationKind::FileManager => {
            command.arg(workspace_path);
        }
        WorkspaceApplicationKind::Terminal => {
            if candidate.app.id == "windows-terminal" {
                command.arg("-d").arg(workspace_path);
            }
            command.current_dir(workspace_path);
        }
    }

    command
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|error| format!("could not start {}: {error}", candidate.app.label))
}

fn resolve_application(
    candidate: &WindowsCandidate,
    registry: &[RegistryApplication],
) -> Option<PathBuf> {
    if candidate.app.id == "explorer" {
        return std::env::var_os("WINDIR")
            .map(PathBuf::from)
            .map(|path| path.join("explorer.exe"))
            .filter(|path| path.is_file())
            .or_else(|| find_on_path(candidate.executable_names));
    }

    for application in registry {
        if !registry_application_matches(application, candidate) {
            continue;
        }
        if let Some(icon_path) = application
            .display_icon
            .as_ref()
            .filter(|path| executable_matches(path, candidate))
        {
            return Some(icon_path.clone());
        }
        if let Some(install_location) = application.install_location.as_ref() {
            for relative in candidate.install_relative_paths {
                let path = install_location.join(relative);
                if path.is_file() {
                    return Some(path);
                }
            }
        }
    }

    known_application_paths(candidate)
        .into_iter()
        .find(|path| path.is_file())
        .or_else(|| find_on_path(candidate.executable_names))
}

fn registry_application_matches(
    application: &RegistryApplication,
    candidate: &WindowsCandidate,
) -> bool {
    if candidate.display_name_prefixes.is_empty()
        || !candidate
            .display_name_prefixes
            .iter()
            .any(|prefix| starts_with_ignore_ascii_case(&application.display_name, prefix))
    {
        return false;
    }
    candidate.publishers.is_empty()
        || candidate
            .publishers
            .iter()
            .any(|publisher| starts_with_ignore_ascii_case(&application.publisher, publisher))
}

fn starts_with_ignore_ascii_case(value: &str, prefix: &str) -> bool {
    value
        .get(..prefix.len())
        .is_some_and(|start| start.eq_ignore_ascii_case(prefix))
}

fn executable_matches(path: &Path, candidate: &WindowsCandidate) -> bool {
    path.is_file()
        && path
            .file_name()
            .and_then(OsStr::to_str)
            .is_some_and(|name| {
                candidate
                    .executable_names
                    .iter()
                    .any(|expected| name.eq_ignore_ascii_case(expected))
            })
}

fn installed_registry_applications() -> Vec<RegistryApplication> {
    const UNINSTALL: &str = r"SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall";
    let mut applications = Vec::new();
    for root in [HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE] {
        for view in [KEY_WOW64_64KEY, KEY_WOW64_32KEY] {
            let root = RegKey::predef(root);
            let Ok(uninstall) = root.open_subkey_with_flags(UNINSTALL, KEY_READ | view) else {
                continue;
            };
            for subkey_name in uninstall.enum_keys().flatten() {
                let Ok(subkey) = uninstall.open_subkey_with_flags(subkey_name, KEY_READ) else {
                    continue;
                };
                let display_name: String = subkey.get_value("DisplayName").unwrap_or_default();
                if display_name.is_empty() {
                    continue;
                }
                let publisher = subkey.get_value("Publisher").unwrap_or_default();
                let display_icon = subkey
                    .get_value::<String, _>("DisplayIcon")
                    .ok()
                    .and_then(|value| clean_display_icon(&value));
                let install_location = subkey
                    .get_value::<String, _>("InstallLocation")
                    .ok()
                    .map(PathBuf::from)
                    .filter(|path| path.is_dir());
                applications.push(RegistryApplication {
                    display_name,
                    publisher,
                    display_icon,
                    install_location,
                });
            }
        }
    }
    applications
}

fn clean_display_icon(value: &str) -> Option<PathBuf> {
    let trimmed = value.trim();
    let without_index = trimmed
        .rsplit_once(',')
        .filter(|(_, suffix)| suffix.trim().parse::<i32>().is_ok())
        .map(|(path, _)| path)
        .unwrap_or(trimmed)
        .trim()
        .trim_matches('"');
    (!without_index.is_empty()).then(|| PathBuf::from(without_index))
}

fn known_application_paths(candidate: &WindowsCandidate) -> Vec<PathBuf> {
    let mut paths = Vec::new();
    let local_app_data = std::env::var_os("LOCALAPPDATA").map(PathBuf::from);
    let program_files = std::env::var_os("ProgramFiles").map(PathBuf::from);
    let program_files_x86 = std::env::var_os("ProgramFiles(x86)").map(PathBuf::from);

    match candidate.app.id {
        "vscode" => {
            if let Some(root) = local_app_data.as_ref() {
                paths.push(root.join(r"Programs\Microsoft VS Code\Code.exe"));
            }
            for root in [program_files.as_ref(), program_files_x86.as_ref()]
                .into_iter()
                .flatten()
            {
                paths.push(root.join(r"Microsoft VS Code\Code.exe"));
            }
        }
        "cursor" => {
            if let Some(root) = local_app_data.as_ref() {
                paths.push(root.join(r"Programs\Cursor\Cursor.exe"));
            }
        }
        "zed" => {
            if let Some(root) = local_app_data.as_ref() {
                paths.push(root.join(r"Programs\Zed\Zed.exe"));
                paths.push(root.join(r"Zed\Zed.exe"));
            }
        }
        "antigravity" => {
            if let Some(root) = local_app_data.as_ref() {
                paths.push(root.join(r"Programs\Antigravity\Antigravity.exe"));
            }
        }
        "windows-terminal" => {
            if let Some(root) = local_app_data.as_ref() {
                paths.push(root.join(r"Microsoft\WindowsApps\wt.exe"));
            }
        }
        "ghostty" => {
            if let Some(root) = local_app_data.as_ref() {
                paths.push(root.join(r"Programs\ghostty\bin\ghostty.exe"));
                paths.push(root.join(r"Programs\Ghostty\ghostty.exe"));
            }
        }
        "warp" => {
            if let Some(root) = local_app_data.as_ref() {
                paths.push(root.join(r"Programs\Warp\Warp.exe"));
            }
        }
        _ => {}
    }
    paths
}

fn find_on_path(names: &[&str]) -> Option<PathBuf> {
    names.iter().find_map(|name| {
        let output = Command::new("where.exe")
            .arg(name)
            .stdin(Stdio::null())
            .stderr(Stdio::null())
            .output()
            .ok()?;
        if !output.status.success() {
            return None;
        }
        String::from_utf8_lossy(&output.stdout)
            .lines()
            .map(str::trim)
            .filter(|line| !line.is_empty())
            .map(PathBuf::from)
            .find(|path| path.is_file())
    })
}

fn app_icon_data_url(executable: &Path) -> Option<String> {
    let rgba = executable_icon_rgba(executable)?;
    let mut png_bytes = Vec::new();
    {
        let mut encoder = png::Encoder::new(&mut png_bytes, rgba.width, rgba.height);
        encoder.set_color(png::ColorType::Rgba);
        encoder.set_depth(png::BitDepth::Eight);
        let mut writer = encoder.write_header().ok()?;
        writer.write_image_data(&rgba.pixels).ok()?;
    }
    Some(format!(
        "data:image/png;base64,{}",
        STANDARD.encode(png_bytes)
    ))
}

struct IconPixels {
    width: u32,
    height: u32,
    pixels: Vec<u8>,
}

struct OwnedIcon(HICON);

impl Drop for OwnedIcon {
    fn drop(&mut self) {
        if !self.0.is_null() {
            // SAFETY: this guard exclusively owns the HICON returned by
            // SHGetFileInfoW and releases it exactly once.
            unsafe {
                DestroyIcon(self.0);
            }
        }
    }
}

struct OwnedBitmap(HBITMAP);

impl Drop for OwnedBitmap {
    fn drop(&mut self) {
        if !self.0.is_null() {
            // SAFETY: ICONINFO transfers bitmap handles to the caller. This
            // guard owns one handle and releases it exactly once.
            unsafe {
                DeleteObject(self.0);
            }
        }
    }
}

struct OwnedDc(HDC);

impl Drop for OwnedDc {
    fn drop(&mut self) {
        if !self.0.is_null() {
            // SAFETY: the DC was acquired with GetDC for the null window and is
            // paired with ReleaseDC using the same null window.
            unsafe {
                ReleaseDC(ptr::null_mut(), self.0);
            }
        }
    }
}

fn executable_icon_rgba(executable: &Path) -> Option<IconPixels> {
    let wide_path: Vec<u16> = executable
        .as_os_str()
        .encode_wide()
        .chain(Some(0))
        .collect();
    let mut extracted_icon: HICON = ptr::null_mut();
    // SAFETY: wide_path is NUL-terminated and extracted_icon points to writable
    // storage for the single large icon requested from the executable.
    let result = unsafe {
        ExtractIconExW(
            wide_path.as_ptr(),
            0,
            &mut extracted_icon,
            ptr::null_mut(),
            1,
        )
    };
    if result == 0 || extracted_icon.is_null() {
        return None;
    }
    let icon = OwnedIcon(extracted_icon);

    let mut info = ICONINFO::default();
    // SAFETY: icon is valid for the lifetime of this call and info is writable.
    if unsafe { GetIconInfo(icon.0, &mut info) } == 0 {
        return None;
    }
    let mask = OwnedBitmap(info.hbmMask);
    let color = OwnedBitmap(info.hbmColor);
    if color.0.is_null() {
        return None;
    }

    let mut bitmap = BITMAP::default();
    // SAFETY: color is a valid bitmap handle and bitmap has sufficient space
    // for the metadata copied by GetObjectW.
    let copied = unsafe {
        GetObjectW(
            color.0,
            mem::size_of::<BITMAP>() as i32,
            (&mut bitmap as *mut BITMAP).cast(),
        )
    };
    if copied != mem::size_of::<BITMAP>() as i32 {
        return None;
    }

    let width = bitmap.bmWidth.unsigned_abs();
    let height = bitmap.bmHeight.unsigned_abs();
    let pixel_count = usize::try_from(width)
        .ok()?
        .checked_mul(usize::try_from(height).ok()?)?;
    let mut bgra = vec![0_u32; pixel_count];
    let dc = OwnedDc(
        // SAFETY: GetDC accepts a null window to acquire the screen DC.
        unsafe { GetDC(ptr::null_mut()) },
    );
    if dc.0.is_null() {
        return None;
    }
    let mut bitmap_info = BITMAPINFO {
        bmiHeader: BITMAPINFOHEADER {
            biSize: mem::size_of::<BITMAPINFOHEADER>() as u32,
            biWidth: bitmap.bmWidth,
            biHeight: -bitmap.bmHeight,
            biPlanes: 1,
            biBitCount: 32,
            biCompression: BI_RGB,
            ..BITMAPINFOHEADER::default()
        },
        ..BITMAPINFO::default()
    };
    // SAFETY: bgra owns enough initialized space for width*height 32-bit
    // pixels, and the handles remain alive for the duration of the call.
    let scan_lines = unsafe {
        GetDIBits(
            dc.0,
            color.0,
            0,
            height,
            bgra.as_mut_ptr().cast(),
            &mut bitmap_info,
            DIB_RGB_COLORS,
        )
    };
    if scan_lines <= 0 || scan_lines as u32 != height {
        return None;
    }

    let mut pixels = Vec::with_capacity(pixel_count.checked_mul(4)?);
    for pixel in bgra {
        let [blue, green, red, alpha] = pixel.to_le_bytes();
        pixels.extend_from_slice(&[red, green, blue, alpha]);
    }
    if pixels.chunks_exact(4).all(|pixel| pixel[3] == 0) {
        for pixel in pixels.chunks_exact_mut(4) {
            pixel[3] = 255;
        }
    }
    drop(mask);
    Some(IconPixels {
        width,
        height,
        pixels,
    })
}

#[cfg(test)]
mod tests {
    use super::{clean_display_icon, registry_application_matches, RegistryApplication};
    use std::path::PathBuf;

    #[test]
    fn cleans_quotes_and_icon_index() {
        assert_eq!(
            clean_display_icon(r#""C:\Program Files\Code\Code.exe",0"#),
            Some(PathBuf::from(r"C:\Program Files\Code\Code.exe"))
        );
    }

    #[test]
    fn validates_registry_identity_before_using_its_path() {
        let application = RegistryApplication {
            display_name: "Microsoft Visual Studio Code (User)".to_string(),
            publisher: "Microsoft Corporation".to_string(),
            display_icon: None,
            install_location: None,
        };
        let vscode = super::APPLICATIONS
            .iter()
            .find(|candidate| candidate.app.id == "vscode")
            .expect("VS Code candidate");
        assert!(registry_application_matches(&application, vscode));

        let spoofed = RegistryApplication {
            publisher: "Unknown Publisher".to_string(),
            ..application
        };
        assert!(!registry_application_matches(&spoofed, vscode));
    }
}
