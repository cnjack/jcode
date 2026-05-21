# JCODE Design System

## Visual Theme and Atmosphere

JCODE embodies "developer tooling elegance." The interface draws from terminal aesthetics and code editors — monospaced accents, structured layouts, and a palette that signals precision. The design philosophy is rooted in transparency and control: every surface is clean, every action is visible, and nothing is hidden behind decorative abstractions.

The visual language borrows from:
- Terminal UIs: monospace fonts for code, dark code blocks, structured command-line previews
- Developer-first products: dense information layout, minimal decoration, functional color coding
- Engineering minimalism: surfaces earn their place through utility, not ornamentation

The atmosphere is professional and technical without feeling cold. The warm orange accent (#FF8400) injects energy and signals action — like a cursor blinking, ready for input. Light and dark modes share the same structural language, with surfaces inverting while maintaining hierarchy.

## Color Palette

| Role | Token | Light Mode | Dark Mode | Usage |
|------|-------|------------|-----------|-------|
| Background | --background | #F2F3F0 | #111111 | Page background, root surface |
| Surface | --card | #FFFFFF | #1A1A1A | Cards, panels, elevated surfaces |
| Primary | --primary | #FF8400 | #FF8400 | Brand accent, badges, CTAs, status indicators |
| Primary Foreground | --primary-foreground | #111111 | #111111 | Text on primary backgrounds |
| Foreground | --foreground | #111111 | #FFFFFF | Primary text, headings |
| Muted Foreground | --muted-foreground | #666666 | #B8B9B6 | Secondary text, descriptions, labels |
| Border | --border | #CBCCC9 | #2E2E2E | Dividers, separators, card borders |
| Muted | --muted | #F2F3F0 | #2E2E2E | Subtle backgrounds, hover states |
| Secondary | --secondary | #E7E8E5 | #2E2E2E | Secondary buttons, subtle surfaces |
| Secondary Foreground | --secondary-foreground | #111111 | #FFFFFF | Text on secondary backgrounds |
| Code Background | — | #1A1A1A | #1A1A1A | Terminal preview, code blocks |
| Code Text | — | #FFFFFF | #FFFFFF | Terminal/command text |
| Code Muted | — | #B8B9B6 | #B8B9B6 | Terminal secondary text, prompts |
| Code Accent | — | #FF8400 | #FF8400 | Terminal status indicators, highlights |
| Success | --color-success | #DFE6E1 | #222924 | Success states, badges |
| Success Foreground | --color-success-foreground | #004D1A | #B6FFCE | Text on success backgrounds |
| Error | --color-error | #E5DCDA | #24100B | Error states, badges |
| Error Foreground | --color-error-foreground | #8C1C00 | #FF5C33 | Text on error backgrounds |
| Warning | --color-warning | #E9E3D8 | #291C0F | Warning states |
| Warning Foreground | --color-warning-foreground | #804200 | #FF8400 | Text on warning backgrounds |
| Info | --color-info | #DFDFE6 | #222229 | Info states |
| Info Foreground | --color-info-foreground | #000066 | #B2B2FF | Text on info backgrounds |
| Destructive | --destructive | #D93C15 | #FF5C33 | Destructive actions |
| White | --white | #FFFFFF | #FFFFFF | Pure white for buttons on dark |
| Black | --black | #000000 | #000000 | Pure black |

### Color Philosophy
- **Orange (#FF8400)** is the signature accent. It represents action, progress, and the "live" state of the terminal. Used sparingly for maximum impact: badges, primary CTAs, status indicators.
- **Monochrome base** (#111111 to #F2F3F0) creates a clean, typographic canvas that lets the orange speak.
- **Code surfaces** are always near-black (#1A1A1A, #111111) regardless of mode, creating a consistent terminal experience.
- **Semantic colors** (success, error, warning) follow a consistent pattern: light backgrounds with dark text in light mode, dark backgrounds with bright text in dark mode.

## Typography

Font families:
- **Primary / Display:** JetBrains Mono — used for the logo, code blocks, terminal previews, and any developer-facing content
- **Secondary / UI:** Geist — used for headings, body text, navigation, and all interface elements
- **Fallback:** Inter — used in documentation page content

### Type Scale

| Level | Font | Size | Weight | Line-height | Letter-spacing | Usage |
|-------|------|------|--------|-------------|----------------|-------|
| Display | Geist | 56px | 700 | 1.1 | -2px | Hero headline |
| H1 | Geist / Inter | 40px | 600 | 1.2 | -0.5px | Section titles |
| H2 | Geist / Inter | 36px | 600 | 1.2 | -0.5px | Page titles (docs) |
| H3 | Geist / Inter | 24px | 600 | 1.3 | 0 | Step titles, card headings |
| H4 | Geist / Inter | 20px | 600 | 1.4 | 0 | Card titles, subsection headings |
| Body Large | Geist / Inter | 18px | 400 | 1.5 | 0 | Hero subtitle, section descriptions |
| Body | Geist / Inter | 16px | 400 | 1.5 | 0 | Documentation body text |
| Body Small | Geist / Inter | 14px | 400 | 1.5 | 0 | Card descriptions, footer links, sidebar items |
| Caption | Geist / Inter | 13px | 400 | 1.4 | 0 | Terminal text, code, status lines |
| Badge | Geist / Inter | 12px | 500 | 1 | 0 | Badges, labels, tags |
| Tiny | Geist / Inter | 11px | 600 | 1 | 0.5px | Section labels, uppercase category headers (docs sidebar) |
| Logo | JetBrains Mono | 20px | 700 | 1 | 0 | Brand logo `[JCODE]` |

### Typography Rules
- **Headlines** use negative letter-spacing (-2px for display, -0.5px for H1/H2) for a tight, engineered feel
- **Body text** uses default letter-spacing for readability
- **Code/terminal text** uses JetBrains Mono at 12-13px, creating a clear separation from UI text
- **Uppercase labels** (section categories, sidebar headers) use 11px with slight letter-spacing for hierarchy
- **Weight hierarchy**: 700 for display/brand, 600 for headings, 500 for badges/labels, 400 for body

## Components

### Button (Primary)
- Background: #111111 (light mode), #FFFFFF text
- Padding: 0px 24px (large), 0px 16px (small)
- Height: 44px (large), 36px (small)
- Border-radius: 6px
- Font: Geist, 15px (large) / 14px (small), weight 400
- Text color: #FFFFFF

### Button (Secondary)
- Background: --card (#FFFFFF / #1A1A1A)
- Padding: 0px 24px
- Height: 44px
- Border-radius: 6px
- Font: Geist, 15px, weight 400
- Text color: --foreground
- Shadow: 0 1px 2px rgba(0,0,0,0.08) (subtle outer shadow)

### Badge
- Background: --primary (#FF8400)
- Text color: --primary-foreground (#111111)
- Padding: 6px 12px (standard), 8px 16px (large)
- Border-radius: 999px (pill)
- Font: Geist/Inter, 12px, weight 500

### Card (Feature)
- Background: --card (#FFFFFF / #1A1A1A)
- Border-radius: 12px
- Padding: 24px
- Width: 320px (fixed in grid)
- Height: 240px
- Layout: vertical, gap 12px
- Contains: icon (24px, --primary color), title (20px, weight 600), description (14px, --muted-foreground)

### Card (Agent/Team)
- Background: --card
- Border-radius: 12px
- Padding: 24px
- Width: 280px
- Height: 180px
- Contains: avatar (32px circle), name, description, status badge

### Terminal / Code Block
- Background: #111111 (fixed dark)
- Border-radius: 8px (inline), 12px (full terminal frame)
- Padding: 14px 20px (inline), 16px 20px (content area)
- Font: JetBrains Mono, 13px (commands), 12px (status)
- Text colors: #FFFFFF (primary), #B8B9B6 (muted), #FF8400 (accent/status)
- Window chrome: 3 dots (#FF5F57 red, #FFBD2E yellow, #28C840 green) + title bar
- Shadow: 0 4px 16px rgba(0,0,0,0.15) for depth

### Input / Search
- Background: --card
- Border-radius: 6px
- Padding: 8px 12px
- Shadow: 0 1px 2px rgba(0,0,0,0.08)
- Contains: search icon (16px, --muted-foreground) + placeholder text

### Status Badge
- Small pill badge for agent/card status
- Padding: 4px 10px
- Border-radius: 999px
- Font: 11px, weight 500
- Variants:
  - Working: background #DFE6E1, text #004D1A
  - Idle: background #E5DCDA, text #8C1C00

### Navbar
- Background: --background
- Height: 64px
- Padding: 0px 48px
- Layout: horizontal, justifyContent: space_between
- Contains: logo (left), nav links (center), CTA button (right)

### Sidebar (Documentation)
- Background: --background
- Width: 260px
- Border: 1px solid --border (right side only)
- Padding: 24px 16px
- Contains: grouped navigation links with section headers
- Active item: bold weight, --primary color, with left indicator bar (3px wide, --primary)
- Section headers: 11px, uppercase, weight 600, --muted-foreground
- Link items: 14px, weight 400, --muted-foreground (inactive) / --primary (active)

### Footer
- Background: --background
- Height: 280px
- Contains: logo + description + command line (left), link columns (center), CTA (right)
- Border-top: 1px solid --border
- Copyright bar: 12px, --muted-foreground

### Web Sidebar (App)
- Background: --card
- Width: 260px
- Border: 1px solid --border (right side only)
- Padding: 16px
- Layout: vertical, gap 12px
- Contains:
  - **User Row**: avatar (28px square, cornerRadius 6px, #111111 bg) + username (14px, --foreground)
  - **New Conversation Button**: cornerRadius 8px, height 36px, 1px --border stroke, centered text (13px, "+ New conversation")
  - **Tabs Row**: "Sessions" (13px, weight 600, --foreground) + "Files" (13px, --muted-foreground), gap 16px
  - **Session List**: vertical list of session items, padding 8px 0
  - **Status Row**: theme indicator + model name (11px, --muted-foreground) + settings icon, justifyContent: space_between
- Session items: cornerRadius 8px, padding 8px 10px, gap 2px vertical
  - Title: JetBrains Mono, 13px, --foreground, truncated
  - Meta: Geist, 11px, --muted-foreground (model + date)
  - Active state: fill --muted background

### Web Topbar (App)
- Background: --card
- Height: 56px
- Padding: 0px 16px
- Layout: horizontal, justifyContent: space_between
- Border-bottom: 1px solid --border
- Left section: menu icon (16px lucide) + breadcrumb path segments (14px, --foreground for current, --muted-foreground for parents)
- Right section: tab pills (Terminal/Changes) + status indicators + avatar (28px circle, #111111 bg)
- Tab pills: cornerRadius 6px, padding 6px 12px, fill --secondary (active) or --card (inactive)

### Chat Interface
- Centered empty state when no messages:
  - Logo: `[JCODE]` in brand style
  - Headline: "What would you like to build?" (20px, weight 600, --foreground)
  - Subtitle: "Send a message to start a conversation..." (14px, --muted-foreground, centered, width 400px)
- Input Area: fill --background, border-top 1px --border, padding 12px 40px 20px 40px
  - Input Field: fill --card, cornerRadius 12px, padding 16px 20px, placeholder text
  - Input Toolbar: horizontal row of action buttons below input

### Input Field (Chat)
- Background: --card
- Border-radius: 12px
- Padding: 16px 20px
- Width: fill_container
- Placeholder: "Ask anything, / for commands"
- Font: Geist, 14px, --muted-foreground

### Terminal Panel (Embedded)
- Background: --card
- Height: 200px
- Border-top: 1px solid --border
- Tab Bar: height 36px, padding 0px 12px, gap 16px
  - Tab label: 12px, weight 600, --foreground
  - Tab actions: plus icon + x icon (12px, --muted-foreground)
- Terminal Content: padding 8px 12px, fill_container
  - Prompt: "$ temp" in JetBrains Mono 13px, --foreground
  - Cursor: 8px wide, 2px tall rectangle, --foreground

### Changes Panel (Side)
- Background: --card
- Width: 400px
- Border-left: 1px solid --border
- Header: "Review" (14px, weight 600) + plus icon, height 48px, padding 0px 16px, border-bottom 1px --border
- Tabs Row: height 36px, padding 0px 16px, gap 16px
  - Tab item: 13px, --foreground, with chevron-down icon (14px)
- Content: vertical layout, padding 16px 0, gap 8px
  - Zero state: "0 Changes" (12px, --muted-foreground)
  - File tree rows: folder rows with chevron + folder icon + name, file rows with file icon + name
  - Folder icon: #FF8400 (Lucide folder)
  - File icon: --muted-foreground (Lucide file-code)
  - Row padding: 4px 16px (folder), 4px 16px 4px 40px (file, indented)

### File Tree
- Background: --card
- Width: 260px
- Border-right: 1px solid --border
- Tree Tabs: height 36px, padding 0px 12px
- Tree Items:
  - Folder Row: chevron-down (12px, --muted-foreground) + folder icon (14px, #FF8400) + name (13px, --foreground), padding 6px 12px, gap 6px
  - File Row: file-code icon (14px, --muted-foreground) + name (13px, --foreground), padding 6px 12px 6px 32px (indented), gap 6px

### Code Editor Panel
- Background: --background
- Flexible width
- Editor Tabs: height 36px, padding 0px 16px, gap 8px
- Breadcrumb: 8px 16px padding, path segments with separator
- Code Content: padding 16px 24px, JetBrains Mono, syntax highlighted

### Message Bubble (User)
- Avatar: 28px square, cornerRadius 6px, #111111 bg, white initial
- Content: --foreground text, Geist 14px
- Layout: horizontal, gap 8px, padding 8px 12px

### Message Bubble (Agent)
- Avatar: colored circle (28px), with agent initial
- Header: agent name + status + timestamp
- Content: --foreground text, Geist 14px
- Code blocks: #111111 bg, JetBrains Mono, cornerRadius 8px

### Tool Call Card
- Background: --card
- Border-radius: 12px
- Padding: 12px 16px
- Header: agent avatar + name + status badge + timestamp
- Content: description of action being performed
- Action Button: cornerRadius 6px, fill --primary, text --primary-foreground

### Tab Pill
- Background: --secondary (active) or --card (inactive)
- Border-radius: 6px
- Padding: 6px 12px
- Font: Geist, 13px, --foreground
- Used in: topbar tab switching, editor tabs

### Collapsed Panel
- Background: --card
- Width: 48px
- Border-left: 1px solid --border
- Padding: 16px 0
- Layout: vertical, gap 16px, alignItems: center
- Contains: expand button (28px square, 6px radius, 1px border) + divider + team icon + status dot

### Search Box (App)
- Background: --muted
- Border-radius: 8px
- Padding: 8px 12px
- Gap: 8px
- Contains: search icon (14px, --muted-foreground) + placeholder text (13px, --muted-foreground) + keyboard shortcut badge
- Shortcut badge: fill --card, cornerRadius 4px, padding 2px 6px

## Layout

- **Base unit:** 4px
- **Spacing scale:** 4, 8, 12, 16, 20, 24, 32, 48, 64, 80, 120
- **Max content width:** 1440px
- **Content padding:** 120px (desktop horizontal), 48px (navbar/footer horizontal)
- **Section vertical padding:** 80px top, 40-80px bottom
- **Card gap:** 16px
- **Border-radius scale:**
  - 6px: buttons, inputs, search boxes
  - 8px: code blocks, inline elements
  - 12px: cards, terminal frames
  - 999px: badges, pills, avatars

### Grid System
- Feature cards: 3-column grid, gap 16px, centered
- Agent cards: 3-column grid, gap 16px, centered
- Workflow steps: 3-column grid, gap 24px, centered
- Footer: 3-column layout (brand | links | CTA), gap 64px

### Page Structure (Marketing)
1. **Navbar** — fixed height 64px, full width
2. **Hero** — centered, vertical layout, gap 24px, padding 80px top
3. **Terminal Preview** — centered, padding 40px vertical
4. **Features** — centered 3x2 card grid, padding 80px top
5. **Workflow** — centered 3-step horizontal layout
6. **Team** — centered 3-card agent visualization
7. **Footer** — full width, border-top, multi-column

### Documentation Layout
- **Navbar:** 64px height, search input, version badge
- **Sidebar:** 260px fixed width, scrollable
- **Content area:** flexible width, padding 48px 64px
- **Breadcrumb:** 12px, --muted-foreground

### Web App Shell Layout
The web app uses a consistent 3-pane or 2-pane shell layout across all views:

```
+------------------+-------------------------------+------------------+
|   Sidebar        |        Topbar (56px)          |   Right Panel    |
|   (260px)        +-------------------------------+   (optional)     |
|                  |                               |   (320-400px)    |
|  - User row      |        Main Content           |                  |
|  - New conv btn  |                               |                  |
|  - Tabs          |  - Chat / Terminal /          |  - Agents        |
|  - Session list  |    Changes / File Editor      |  - Changes       |
|  - Status row    |                               |                  |
+------------------+-------------------------------+------------------+
```

- **Sidebar:** 260px fixed width, --card background, right border 1px --border
- **Topbar:** 56px fixed height, --card background, bottom border 1px --border
- **Main Content:** flexible width, --background
- **Right Panel:** 320px (agents) or 400px (changes), --card background, left border 1px --border
- **Collapsed Panel:** 48px width, icons only, --card background

### Web App Views
1. **Chat View** — sidebar + topbar + centered empty state (logo + headline + subtitle) + input bar at bottom
2. **Terminal View** — chat view + embedded terminal panel (200px) at bottom
3. **Changes View** — chat view + right changes panel (400px) showing file tree
4. **File View** — sidebar + topbar + file tree panel (260px) + code editor panel (flexible)
5. **Multi-Agent View** — sidebar + topbar + chat column (flexible) + agents panel (320px)
6. **Collapsed Agents** — sidebar + chat + collapsed right panel (48px)

## Depth and Elevation

JCODE uses a restrained shadow system. Most surfaces are flat; depth is created through borders and subtle shadows only when necessary.

| Level | Usage | Shadow |
|-------|-------|--------|
| Level 0 | Page background, flat cards | none |
| Level 1 | Secondary buttons, search inputs | 0 1px 2px rgba(0,0,0,0.08) |
| Level 2 | Terminal frame | 0 4px 16px rgba(0,0,0,0.15) |
| Level 3 | Feature badge | 0 1px 2px rgba(0,0,0,0.08) |

### Elevation Philosophy
- **Prefer borders over shadows** for separation. Most cards have no shadow; they sit on the background through color contrast alone.
- **Shadows are used sparingly** for interactive elements that need to "lift" — secondary buttons, search inputs, terminal frames.
- **Terminal frame** gets the strongest shadow because it represents an embedded application window.
- **No blur-heavy shadows** — all shadows are tight and subtle, maintaining the technical/developer aesthetic.

## Do's and Don'ts

### Do:
- Use JetBrains Mono for code, terminal content, and the brand logo
- Use Geist for all UI text, headings, and body copy
- Use #FF8400 orange sparingly — only for primary CTAs, badges, and status indicators
- Keep code blocks and terminal previews dark (#111111 or #1A1A1A) regardless of theme mode
- Use pill-shaped badges (border-radius: 999px) for labels and tags
- Use 6px border-radius for buttons, 12px for cards
- Maintain tight letter-spacing on headlines (-2px for display)
- Use --muted-foreground for secondary/descriptive text
- Use 1px solid --border for dividers and separators
- Use 1px borders (not shadows) to separate sidebar, topbar, and panels
- Use --card background for sidebar and topbar to create surface hierarchy
- Use --background for main content area to distinguish from panels
- Keep the app shell consistent: 260px sidebar, 56px topbar, flexible main content
- Use JetBrains Mono for session titles and code-related labels in the web app
- Use folder icons in #FF8400 orange for file tree folder rows
- Use chevron-down icons for expandable tree sections
- Use Lucide icons consistently across the web app interface

### Don't:
- Don't use gradients — the design is flat and typographic
- Don't use heavy drop shadows on cards — prefer flat surfaces
- Don't mix more than 2 font families on a single page
- Don't use rounded corners larger than 12px on rectangular elements
- Don't use orange for body text or large background areas
- Don't use shadows without a specific purpose — most surfaces are flat
- Don't use different border-radius values within the same component category
- Don't use decorative elements that don't serve a functional purpose
- Don't make the sidebar wider than 260px — it's a fixed width panel
- Don't use borders on all sides of panels — only the separating edge gets a border
- Don't use different heights for topbars across views — keep it consistent at 56px or 64px

## Responsive Behavior

| Breakpoint | Width | Behavior |
|------------|-------|----------|
| Mobile | < 640px | Single column, stacked cards, hamburger nav, reduced padding |
| Tablet | < 1024px | 2-column grids, sidebar collapses to overlay (docs), medium padding |
| Desktop | >= 1024px | Full layout, 3-column grids, persistent sidebar, 120px horizontal padding |

- **Touch target minimum:** 44x44px
- **Font sizes** don't drop below 13px on mobile
- **Card grids** collapse: 3-col -> 2-col -> 1-col
- **Hero headline** scales down: 56px -> 40px -> 32px
- **Section padding** reduces: 120px -> 64px -> 24px
- **Documentation sidebar** becomes collapsible drawer on tablet/mobile

## Agent Prompt Guide

Quick palette: bg=#F2F3F0 (light) / #111111 (dark), surface=#FFFFFF / #1A1A1A, accent=#FF8400, text=#111111 / #FFFFFF, muted=#666666 / #B8B9B6

### Ready-to-use prompts:

- **"Create a landing page hero"** — centered vertical layout, display headline (56px, weight 700, -2px tracking), subtitle (18px, --muted-foreground), primary CTA button (dark bg, white text, 6px radius) + secondary button (card bg, subtle shadow), optional install command line (dark bg, monospace font)

- **"Build a feature card grid"** — 3-column grid, gap 16px, each card: 320px width, 12px radius, --card background, 24px padding, contains: icon (24px, --primary color), title (20px, weight 600), description (14px, --muted-foreground)

- **"Create a terminal preview"** — dark frame (#111111, 12px radius), window chrome with 3 colored dots (red #FF5F57, yellow #FFBD2E, green #28C840), JetBrains Mono text, status line in #FF8400, content padding 16px 20px, subtle shadow

- **"Build a documentation page"** — 64px navbar with search input + version badge, 260px sidebar with grouped navigation (11px uppercase headers, 14px links, active state with --primary color and left indicator), flexible content area with breadcrumb, headings, and code blocks

- **"Create a workflow/step section"** — 3-column horizontal layout, each step: numbered circle (48px, --primary bg, white text), title (24px, weight 600), description (14px, centered, --muted-foreground)

- **"Build an agent team visualization"** — 3-column card grid, each card: avatar circle (32px, colored), agent name (16px, weight 600), description (14px), status badge (pill, color-coded: working=green-tinted, idle=red-tinted)

- **"Create a footer"** — border-top 1px --border, 3-column layout: brand column (logo + description + command line), links columns (grouped links with 14px --muted-foreground text), CTA column (heading + button), copyright bar at bottom

### Web App Prompts:

- **"Build a chat app shell"** — 260px sidebar (--card bg, right border) + flexible main area. Sidebar contains: user row (28px avatar + username), "New conversation" button (36px height, 8px radius, 1px border), Sessions/Files tabs (13px, gap 16px), session list (vertical, 8px gap, items with JetBrains Mono title 13px + Geist meta 11px), status row at bottom. Main area: 56px topbar (--card bg, bottom border, breadcrumb left, tabs + avatar right) + chat content + input bar at bottom

- **"Create a web sidebar"** — width 260px, --card background, right border 1px --border, padding 16px, vertical layout gap 12px. Contains: user row (28px square avatar #111111 bg + "root" 14px), new conversation button (36px, 8px radius, 1px --border stroke, "+" icon + "New conversation" 13px), tabs row ("Sessions" weight 600 + "Files" --muted-foreground, gap 16px), session list items (8px radius, 8px 10px padding, title in JetBrains Mono 13px, meta in Geist 11px --muted-foreground, active state with --muted fill), status row (theme icon + model name 11px + settings icon)

- **"Build a web topbar"** — height 56px, --card background, bottom border 1px --border, padding 0 16px, horizontal justifyContent space_between. Left: menu icon (16px lucide) + breadcrumb segments (current 14px --foreground, parents 14px --muted-foreground). Right: tab pills (Terminal/Changes, 6px radius, 6px 12px padding, --secondary active / --card inactive) + status frame + avatar (28px circle, #111111 bg)

- **"Create a chat input bar"** — fill --background, border-top 1px --border, padding 12px 40px 20px 40px. Contains: input field (--card bg, 12px radius, 16px 20px padding, placeholder "Ask anything, / for commands") + toolbar row below with action buttons (Agent, Model selector, etc.)

- **"Build an embedded terminal panel"** — fill --card, height 200px, border-top 1px --border. Tab bar (36px height, "Terminal 1" 12px weight 600, plus + x icons 12px). Content area: padding 8px 12px, JetBrains Mono 13px prompt "$ temp" + blinking cursor (8px wide, 2px tall, --foreground)

- **"Create a changes/review panel"** — width 400px, --card bg, left border 1px --border. Header: "Review" 14px weight 600 + plus icon, height 48px, border-bottom 1px --border. Tabs: "Last turn changes" 13px with chevron-down icon. Content: file tree with folder rows (chevron 12px + folder icon 14px #FF8400 + name 13px, padding 4px 16px) and file rows (file-code icon 14px + name 13px, padding 4px 16px 4px 40px indented)

- **"Build a file tree panel"** — width 260px, --card bg, right border 1px --border. Tree tabs at top (36px height). Folder rows: chevron-down 12px + folder icon 14px #FF8400 + name 13px --foreground, padding 6px 12px. File rows: file-code icon 14px --muted-foreground + name 13px --foreground, padding 6px 12px 6px 32px (indented)

- **"Create a code editor view"** — file tree panel (260px, --card, right border) + code editor panel (flexible, --background). Editor: tabs row (36px, 0 16px padding) + breadcrumb (8px 16px padding) + divider + code content (16px 24px padding, JetBrains Mono, syntax highlighted)

- **"Build a multi-agent panel"** — width 320px, --card bg, left border 1px --border. Header: "Panel" title + filter icons, border-bottom 1px --border, padding 16px 20px. Filter row: pills for filtering agents, border-bottom 1px --border, padding 10px 20px. Agent list: vertical cards with avatar + name + status + description, padding 12px. Divider section: "Tool Calls" header with border-top/bottom. Call list: tool call items with timestamp and status

- **"Create a collapsed side panel"** — width 48px, --card bg, left border 1px --border, padding 16px 0, vertical layout gap 16px, alignItems center. Contains: expand button (28px square, 6px radius, 1px border, panel-right-open icon) + divider (20px wide line) + team/users icon + status dot (6px circle, green for active)

- **"Build a search box"** — background --muted, cornerRadius 8px, padding 8px 12px, gap 8px. Contains: search icon (14px, --muted-foreground) + placeholder text (13px, --muted-foreground) + keyboard shortcut badge (--card bg, 4px radius, 2px 6px padding)
