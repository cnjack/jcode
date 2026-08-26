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
    if detected_image_media_type(&bytes) != Some(media_type) {
        return Ok(None);
    }
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

fn detected_image_media_type(data: &[u8]) -> Option<&'static str> {
    if data.starts_with(b"\x89PNG\r\n\x1a\n") {
        return Some("image/png");
    }
    if data.starts_with(&[0xff, 0xd8, 0xff]) {
        return Some("image/jpeg");
    }
    if data.starts_with(b"GIF87a") || data.starts_with(b"GIF89a") {
        return Some("image/gif");
    }
    if data.len() >= 12 && &data[..4] == b"RIFF" && &data[8..12] == b"WEBP" {
        return Some("image/webp");
    }
    None
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
        let bytes = b"\x89PNG\r\n\x1a\npng-bytes";
        fs::write(&path, bytes).expect("write image");

        let image = read_dropped_image(path.to_string_lossy().into_owned())
            .expect("read dropped image")
            .expect("supported image");

        assert_eq!(image.media_type, "image/png");
        assert_eq!(image.name, "sample.png");
        assert_eq!(image.data, STANDARD.encode(bytes));
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

    #[test]
    fn leaves_invalid_or_mismatched_images_for_prompt_fallback() {
        let dir = tempfile::tempdir().expect("tempdir");
        let fake = dir.path().join("fake.png");
        fs::write(&fake, b"not-an-image").expect("write fake image");
        assert!(read_dropped_image(fake.to_string_lossy().into_owned())
            .expect("classify fake image")
            .is_none());

        let mismatch = dir.path().join("mismatch.jpg");
        fs::write(&mismatch, b"\x89PNG\r\n\x1a\npng-bytes").expect("write mismatch");
        assert!(read_dropped_image(mismatch.to_string_lossy().into_owned())
            .expect("classify mismatched image")
            .is_none());
    }
}
