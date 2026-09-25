use std::path::Path;

use tauri::AppHandle;
use tauri_plugin_dialog::DialogExt;

#[tauri::command(rename_all = "camelCase")]
pub async fn save_download_as(
    app: AppHandle,
    window: tauri::Window,
    file_name: String,
    data: Vec<u8>,
) -> Result<bool, String> {
    let file_name = safe_file_name(&file_name);
    let Some(file_path) = app
        .dialog()
        .file()
        .set_parent(&window)
        .set_file_name(file_name)
        .blocking_save_file()
    else {
        return Ok(false);
    };
    let path = file_path
        .into_path()
        .map_err(|error| format!("invalid save destination: {error}"))?;
    let display_path = path.display().to_string();
    tauri::async_runtime::spawn_blocking(move || std::fs::write(&path, data))
        .await
        .map_err(|error| format!("failed to save {display_path}: {error}"))?
        .map_err(|error| format!("failed to save {display_path}: {error}"))?;
    Ok(true)
}

fn safe_file_name(file_name: &str) -> &str {
    let base_name = file_name
        .rsplit(|character| character == '/' || character == '\\')
        .next()
        .unwrap_or("");
    if base_name.trim().is_empty() || Path::new(base_name).file_name().is_none() {
        "download"
    } else {
        base_name
    }
}

#[cfg(test)]
mod tests {
    use super::safe_file_name;

    #[test]
    fn save_name_is_reduced_to_a_file_name() {
        assert_eq!(safe_file_name("../reports/sales.xlsx"), "sales.xlsx");
        assert_eq!(safe_file_name(r"..\reports\sales.xlsx"), "sales.xlsx");
        assert_eq!(safe_file_name(""), "download");
    }
}
