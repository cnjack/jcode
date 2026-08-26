//! Narrow bridge for turning user-dropped image paths into chat attachments.
//!
//! Tauri's native drag/drop event exposes absolute paths, while the webview's
//! `File` objects do not. Keep arbitrary files as paths in the prompt; only
//! read the image formats already supported by the chat composer, with the
//! same 10 MiB ceiling as browser-selected images.

use std::fs;
use std::path::Path;

use base64::{engine::general_purpose::STANDARD, Engine as _};
use serde::Serialize;

const MAX_DROPPED_IMAGE_BYTES: u64 = 10 * 1024 * 1024;

#[derive(Debug, Serialize)]
pub struct DroppedImage {
    data: String,
    media_type: &'static str,
    name: String,
}

#[tauri::command]
pub fn read_dropped_image(path: String) -> Result<Option<DroppedImage>, String> {
    let path = Path::new(&path);
    let Some(media_type) = image_media_type(path) else {
        return Ok(None);
    };
    let metadata = match fs::metadata(path) {
        Ok(metadata) if metadata.is_file() => metadata,
        Ok(_) | Err(_) => return Ok(None),
    };
    if metadata.len() > MAX_DROPPED_IMAGE_BYTES {
        return Ok(None);
    }

    let bytes = fs::read(path).map_err(|error| format!("read dropped image: {error}"))?;
    let name = path
        .file_name()
        .map(|value| value.to_string_lossy().into_owned())
        .unwrap_or_else(|| "attachment".to_owned());
    Ok(Some(DroppedImage {
        data: STANDARD.encode(bytes),
        media_type,
        name,
    }))
}

fn image_media_type(path: &Path) -> Option<&'static str> {
    match path
        .extension()?
        .to_string_lossy()
        .to_ascii_lowercase()
        .as_str()
    {
        "png" => Some("image/png"),
        "jpg" | "jpeg" => Some("image/jpeg"),
        "gif" => Some("image/gif"),
        "webp" => Some("image/webp"),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_supported_image_as_base64() {
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("sample.png");
        fs::write(&path, b"png-bytes").expect("write image");

        let image = read_dropped_image(path.to_string_lossy().into_owned())
            .expect("read dropped image")
            .expect("supported image");

        assert_eq!(image.media_type, "image/png");
        assert_eq!(image.name, "sample.png");
        assert_eq!(image.data, STANDARD.encode(b"png-bytes"));
    }

    #[test]
    fn leaves_non_images_for_prompt_fallback() {
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("notes.pdf");
        fs::write(&path, b"pdf").expect("write file");

        assert!(read_dropped_image(path.to_string_lossy().into_owned())
            .expect("classify dropped file")
            .is_none());
    }

    #[test]
    fn leaves_oversized_images_for_prompt_fallback() {
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("large.webp");
        let file = fs::File::create(&path).expect("create image");
        file.set_len(MAX_DROPPED_IMAGE_BYTES + 1)
            .expect("size image");

        assert!(read_dropped_image(path.to_string_lossy().into_owned())
            .expect("classify dropped image")
            .is_none());
    }
}
