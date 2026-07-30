use super::WorkspaceApplication;
use std::path::Path;

pub fn list_workspace_applications() -> Vec<WorkspaceApplication> {
    Vec::new()
}

pub fn open_workspace(_workspace_path: &Path, _app_id: &str) -> Result<(), String> {
    Err("workspace applications are unavailable on this platform".to_string())
}
