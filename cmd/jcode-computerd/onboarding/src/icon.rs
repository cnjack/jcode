//! The helper's own icon, drawn in code.
//!
//! jcode's main icon is a white tile with the orange "J" + dark `</>`; the
//! helper must be visually *distinct* (it is a separate row in System
//! Settings) while still reading as family. So this inverts the scheme: a
//! full-bleed brand-orange gradient tile (brand #FF8400, internal/theme
//! palette.go) with a white cursor arrow — the same relationship Codex uses
//! between "Codex" and "Codex Computer Use".
//!
//! Every size is re-rendered from vectors (no downscaling), so the 16 px
//! favicon-size glyph stays crisp.

use std::path::Path;
use tiny_skia::{
    Color, FillRule, GradientStop, LinearGradient, Paint, PathBuilder, Pixmap, Point, SpreadMode,
    Transform,
};

/// Apple `.iconset` members: (file name, pixel size).
const ICONSET: &[(&str, u32)] = &[
    ("icon_16x16.png", 16),
    ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32),
    ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128),
    ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256),
    ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512),
    ("icon_512x512@2x.png", 1024),
];

pub fn render_iconset(dir: &Path) -> Result<(), String> {
    std::fs::create_dir_all(dir).map_err(|e| format!("create {}: {e}", dir.display()))?;
    for (name, px) in ICONSET {
        let pixmap = render(*px)?;
        let path = dir.join(name);
        pixmap
            .save_png(&path)
            .map_err(|e| format!("write {}: {e}", path.display()))?;
    }
    Ok(())
}

pub fn render_single(path: &Path, px: u32) -> Result<(), String> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| format!("create {}: {e}", parent.display()))?;
    }
    render(px)?
        .save_png(path)
        .map_err(|e| format!("write {}: {e}", path.display()))
}

/// All coordinates below are authored on a 1024-point canvas and scaled.
fn render(px: u32) -> Result<Pixmap, String> {
    let mut pixmap = Pixmap::new(px, px).ok_or("pixmap alloc failed")?;
    let s = px as f32 / 1024.0;

    // ── Tile: the macOS Big Sur icon grid — 824×824 squircle centered on a
    // 1024 canvas with a transparent margin, so it sits at the same optical
    // size as every other app icon in the TCC list.
    let margin = 100.0 * s;
    let tile = 824.0 * s;
    let radius = 186.0 * s;

    let mut paint = Paint::default();
    paint.anti_alias = true;
    paint.shader = LinearGradient::new(
        Point::from_xy(margin, margin),
        Point::from_xy(margin + tile, margin + tile),
        vec![
            GradientStop::new(0.0, rgb(0xFF, 0xB2, 0x58)),
            GradientStop::new(0.52, rgb(0xFF, 0x84, 0x00)),
            GradientStop::new(1.0, rgb(0xEE, 0x63, 0x00)),
        ],
        SpreadMode::Pad,
        Transform::identity(),
    )
    .ok_or("gradient")?;
    let tile_path = rounded_rect(margin, margin, tile, tile, radius).ok_or("tile path")?;
    pixmap.fill_path(&tile_path, &paint, FillRule::Winding, Transform::identity(), None);

    // ── Pixel-square echo: the main jcode icon scatters small orange squares
    // beside the "J"; the helper echoes them in translucent white so the two
    // icons read as one family at a glance.
    let mut white_soft = Paint::default();
    white_soft.anti_alias = true;
    white_soft.set_color(Color::from_rgba8(0xFF, 0xFF, 0xFF, 0xD9));
    for (x, y, sz) in [(636.0, 236.0, 58.0), (724.0, 300.0, 40.0), (664.0, 344.0, 26.0)] {
        if let Some(sq) = rounded_rect(x * s, y * s, sz * s, sz * s, sz * s * 0.22) {
            pixmap.fill_path(&sq, &white_soft, FillRule::Winding, Transform::identity(), None);
        }
    }

    // ── Cursor arrow: the classic pointer (vertical left edge, ~45° right
    // edge, offset tail), filled white. Unit polygon, y-down.
    const ARROW: &[(f32, f32)] = &[
        (0.000, 0.000), // tip
        (0.000, 0.727), // bottom of the vertical left edge
        (0.170, 0.563), // base notch (tail's left shoulder)
        (0.290, 0.847), // tail bottom-left
        (0.413, 0.794), // tail bottom-right
        (0.291, 0.510), // tail top-right, under the head
        (0.472, 0.470), // right wing of the head
    ];
    let scale = 500.0 * s;
    let (bw, bh) = (0.472 * scale, 0.847 * scale);
    let ox = (px as f32 - bw) / 2.0 - 30.0 * s; // optical: mass sits low-right of the tip
    let oy = (px as f32 - bh) / 2.0 + 10.0 * s;

    let mut pb = PathBuilder::new();
    pb.move_to(ox + ARROW[0].0 * scale, oy + ARROW[0].1 * scale);
    for (x, y) in &ARROW[1..] {
        pb.line_to(ox + x * scale, oy + y * scale);
    }
    pb.close();
    let arrow = pb.finish().ok_or("arrow path")?;

    // Soft grounding: a slightly larger dark copy behind the arrow stands in
    // for a blur (tiny-skia has none) — subtle enough to read as depth.
    let mut shadow = Paint::default();
    shadow.anti_alias = true;
    shadow.set_color(Color::from_rgba8(0x7A, 0x2E, 0x00, 0x38));
    pixmap.fill_path(
        &arrow,
        &shadow,
        FillRule::Winding,
        Transform::from_translate(6.0 * s, 12.0 * s),
        None,
    );

    let mut white = Paint::default();
    white.anti_alias = true;
    white.set_color(Color::from_rgba8(0xFF, 0xFF, 0xFF, 0xFF));
    pixmap.fill_path(&arrow, &white, FillRule::Winding, Transform::identity(), None);

    Ok(pixmap)
}

fn rgb(r: u8, g: u8, b: u8) -> Color {
    Color::from_rgba8(r, g, b, 0xFF)
}

/// Rounded rectangle with circular corners (cubic approximation, κ≈0.5523).
fn rounded_rect(x: f32, y: f32, w: f32, h: f32, r: f32) -> Option<tiny_skia::Path> {
    let r = r.min(w / 2.0).min(h / 2.0);
    let k = r * 0.5523;
    let mut pb = PathBuilder::new();
    pb.move_to(x + r, y);
    pb.line_to(x + w - r, y);
    pb.cubic_to(x + w - r + k, y, x + w, y + r - k, x + w, y + r);
    pb.line_to(x + w, y + h - r);
    pb.cubic_to(x + w, y + h - r + k, x + w - r + k, y + h, x + w - r, y + h);
    pb.line_to(x + r, y + h);
    pb.cubic_to(x + r - k, y + h, x, y + h - r + k, x, y + h - r);
    pb.line_to(x, y + r);
    pb.cubic_to(x, y + r - k, x + r - k, y, x + r, y);
    pb.close();
    pb.finish()
}
