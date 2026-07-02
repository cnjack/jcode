export type ShowcaseKind = 'challenge' | 'sprint'

export interface ShowcaseEntry {
  id: string
  kind: ShowcaseKind
  title: string
  tagline: string
  description: string
  tags: string[]
  /** relative to site root, served from public/ */
  src: string
  accent: string
  /** build provenance — every project here was written by the jcode agent itself */
  build: {
    model: string
    rounds: number
    note: string
  }
  highlights: string[]
  /** aspect ratio hint for the card thumbnail iframe */
  tall?: boolean
}

export const CHALLENGES: ShowcaseEntry[] = [
  {
    id: 'city3d',
    kind: 'challenge',
    title: 'Neon Metropolis',
    tagline: 'A cinematic procedural 3D city — day/night, living traffic and a one-key flythrough, all from a single seed',
    description:
      'jcode built a full procedural city in three.js over six rounds (2h17m of agent time): ~500 instanced buildings across four seeded districts, a complete day/night cycle with stars and drifting clouds, animated traffic with headlights, street lamps, district labels, quality presets and a cinematic spline flythrough — fully static, zero external requests, 60fps.',
    tags: ['three.js', 'WebGL', 'procedural'],
    src: '/showcase-projects/city3d/index.html',
    accent: '#7db2ff',
    build: {
      model: 'GLM-5.1',
      rounds: 6,
      note: 'Three feature rounds (city core → day/night + traffic + controls → cinematics & atmosphere) plus three review-driven fix rounds — including a three-r160 shader migration and one leetspeak hex constant (0xc10ud01) that a human reviewer diagnosed and jcode fixed.',
    },
    highlights: [
      '~500 instanced buildings across four seeded districts — deterministic from a visible seed',
      'Full day/night cycle: stars, clouds, window grids and street lamps that ignite at dusk',
      'Cinematic spline flythrough through street canyons at 60fps — press C',
    ],
  },
  {
    id: 'webgpu-cnn',
    kind: 'challenge',
    title: 'NeuraLab — train a CNN in your browser',
    tagline: 'A real convolutional network training live in the browser — WebGPU compute shaders with a pure-JS CPU fallback',
    description:
      'jcode built an interactive ML lab over seven rounds (~3h of agent time): it procedurally generates its own digit dataset, trains a hand-written CNN live (17 WGSL compute kernels on WebGPU, ~12× faster than the pure-JS CPU backend), charts loss and accuracy, visualizes filters and activations, and classifies digits you draw. Nothing is faked — gradient checks and a CPU↔GPU parity test run in-page.',
    tags: ['WebGPU', 'WGSL', 'machine learning'],
    src: '/showcase-projects/webgpu-cnn/index.html',
    accent: '#8ce99a',
    build: {
      model: 'GLM-5.1',
      rounds: 7,
      note: 'Three feature rounds (CPU trainer → WebGPU backend → inference playground) plus four review-driven fix rounds — including a buffer-aliasing NaN bug and Dawn rejecting module-scope `let` in WGSL, both root-caused by jcode itself.',
    },
    highlights: [
      'Hand-written backprop verified by an in-page numerical gradient check (max rel err 1.3e-5)',
      '17 WGSL compute kernels train the same network ~12× faster, with a one-click CPU↔GPU parity test (5.4e-7)',
      'Draw a digit for live probabilities, filters and activation maps — 97% test accuracy after a minute',
    ],
  },
  {
    id: 'ui-kit',
    kind: 'challenge',
    title: 'Jui — a zero-dependency UI kit',
    tagline: 'A ~30-component design system with its own docs site — tokens, dark mode, a11y-correct overlays and a ⌘K palette',
    description:
      'jcode designed, built and documented "Jui" in pure HTML/CSS/vanilla JS: a token-driven theming engine, light/dark themes, thirty-plus components from buttons to a command palette — each with live demos and copyable snippets — plus a SaaS dashboard composed entirely from the kit. The docs site itself is rendered with Jui.',
    tags: ['design system', 'CSS', 'accessibility'],
    src: '/showcase-projects/ui-kit/index.html',
    accent: '#ffd43b',
    build: {
      model: 'GLM-5.1',
      rounds: 3,
      note: 'Three verified rounds: tokens + 9 core components → 11 interactive components (focus traps, ARIA) → data components, ⌘K palette, theme customizer and dashboard. One real a11y bug (overlay focus timing) was found in review and fixed by jcode in the next round.',
    },
    highlights: [
      '30 components, 0 dependencies — one CSS file, one JS file, no build step',
      'Real accessibility: focus-trapped modals, roving-tabindex tabs, typeahead select, aria-live toasts',
      'Live theme engine: accent hue, radius & density sliders restyle every component instantly',
    ],
    tall: true,
  },
  {
    id: 'game',
    kind: 'challenge',
    title: 'Circuit Defense',
    tagline: 'Bugs crawl the PCB traces toward your CPU — solder chips, hold 20 waves, then go endless',
    description:
      'A complete, genuinely playable tower-defense game by jcode: four upgradeable chip-towers with targeting modes, six bug types including armored tanks, splitting swarms and bosses, three hand-laid circuit maps, three difficulties, endless mode, persistent best scores and a fully synthesized WebAudio soundtrack — zero dependencies, zero asset files.',
    tags: ['canvas', 'game', 'WebAudio'],
    src: '/showcase-projects/game/index.html',
    accent: '#f783ac',
    build: {
      model: 'GLM-5.1',
      rounds: 3,
      note: 'Core loop & waves → depth (upgrades, bosses, juice) → sound, maps, meta & balance. QA played full 20-wave runs through a scripted debug API; a broken mouse hit-test found in review was root-caused and regression-tested by jcode itself.',
    },
    highlights: [
      'Full TD depth: 4 towers × 3 upgrade tiers, 4 targeting modes, 6 enemy types, 3 maps, endless mode',
      '100% procedural: canvas-drawn PCB art, WebAudio-synthesized sound — no assets, no build step',
      'Deterministic 60Hz fixed-timestep sim — QA played 20-wave runs in milliseconds',
    ],
  },
]

export const SPRINTS: ShowcaseEntry[] = [
  {
    id: 'fe_landing_hero',
    kind: 'sprint',
    title: 'SaaS Landing Page',
    tagline: 'Marketing page with hero, features and pricing',
    description: 'A single-prompt landing page: hero, feature grid, testimonials and pricing table.',
    tags: ['HTML/CSS'],
    src: '/showcase-projects/fe_landing_hero/index.html',
    accent: '#e96f1e',
    build: { model: 'GLM-5.1', rounds: 1, note: 'One-shot task from the unattended eval suite.' },
    highlights: [],
  },
  {
    id: 'fe_analytics_dashboard',
    kind: 'sprint',
    title: 'Analytics Dashboard',
    tagline: 'KPI cards, charts and tables',
    description: 'A dashboard layout with stat cards, hand-drawn SVG charts and a data table.',
    tags: ['dashboard'],
    src: '/showcase-projects/fe_analytics_dashboard/index.html',
    accent: '#7db2ff',
    build: { model: 'GLM-5.1', rounds: 1, note: 'One-shot task from the unattended eval suite.' },
    highlights: [],
  },
  {
    id: 'fe_todo_app',
    kind: 'sprint',
    title: 'Todo App',
    tagline: 'Local-storage todo with filters',
    description: 'Classic todo app: add, complete, filter, persist — no frameworks.',
    tags: ['vanilla JS'],
    src: '/showcase-projects/fe_todo_app/index.html',
    accent: '#8ce99a',
    build: { model: 'GLM-5.1', rounds: 1, note: 'One-shot task from the unattended eval suite.' },
    highlights: [],
  },
  {
    id: 'fe_pricing_calculator',
    kind: 'sprint',
    title: 'Pricing Calculator',
    tagline: 'Interactive tiered pricing widget',
    description: 'Sliders and toggles compute a live price with tier breakdowns.',
    tags: ['widget'],
    src: '/showcase-projects/fe_pricing_calculator/index.html',
    accent: '#ffd43b',
    build: { model: 'GLM-5.1', rounds: 1, note: 'One-shot task from the unattended eval suite.' },
    highlights: [],
  },
  {
    id: 'fe_canvas_particles',
    kind: 'sprint',
    title: 'Particle Field',
    tagline: 'Interactive canvas particle system',
    description: 'A mouse-reactive particle field with connection lines and trails.',
    tags: ['canvas'],
    src: '/showcase-projects/fe_canvas_particles/index.html',
    accent: '#f783ac',
    build: { model: 'GLM-5.1', rounds: 1, note: 'One-shot task from the unattended eval suite.' },
    highlights: [],
  },
  {
    id: 'fe_svg_dataviz',
    kind: 'sprint',
    title: 'SVG Data Viz',
    tagline: 'Animated donut chart with legend',
    description: 'An animated SVG donut chart with hover states and a synced legend.',
    tags: ['SVG'],
    src: '/showcase-projects/fe_svg_dataviz/index.html',
    accent: '#b197fc',
    build: { model: 'GLM-5.1', rounds: 1, note: 'One-shot task from the unattended eval suite.' },
    highlights: [],
  },
]

export const ALL_PROJECTS = [...CHALLENGES, ...SPRINTS]

export function findProject(id: string): ShowcaseEntry | undefined {
  return ALL_PROJECTS.find((p) => p.id === id)
}
