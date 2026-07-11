/**
 * FileTreeRenderer — `list_dir` / `glob`. Parses a line-separated path list
 * (tolerant of leading bullets / tree glyphs) into a collapsible tree.
 *
 * - Directories collapse/expand (chevron + folder icon).
 * - Files get an extension-colored dot (colors resolve to theme tokens in p5.css).
 * - Rows highlight on hover.
 * - Default: fully expanded. When the tree exceeds 200 nodes, only the top
 *   level stays open (second level and below collapse) to keep it scannable.
 */

import { memo, useMemo, useState } from 'react'
import { ChevronRightIcon, FolderIcon, FolderOpenIcon } from '@heroicons/react/24/outline'
import type { ToolRendererProps } from 'jcode-ui-core/adapters'

interface TreeNode {
  name: string
  path: string
  isDir: boolean
  children: TreeNode[]
}

const LARGE_TREE_THRESHOLD = 200

export const FileTreeRenderer = memo(function FileTreeRenderer({
  output,
  displayOutput,
  error,
  status,
}: ToolRendererProps) {
  const raw = displayOutput || output || ''
  const { roots, count } = useMemo(() => parsePathList(raw), [raw])

  if (roots.length === 0) {
    if (error) {
      return (
        <div className="jcode-filetree__msg jcode-filetree__msg--error">{error}</div>
      )
    }
    if (status === 'running') {
      return <div className="jcode-filetree__msg animate-pulse">Listing…</div>
    }
    if (raw.trim()) {
      // Parsed nothing but there is text — show it raw rather than an empty box.
      return <pre className="jcode-filetree__raw">{raw}</pre>
    }
    return <div className="jcode-filetree__msg">Empty</div>
  }

  // >200 nodes: keep only the top level open.
  const initialOpenDepth = count > LARGE_TREE_THRESHOLD ? 1 : Infinity

  return (
    <div data-jcode-ui="" className="jcode-filetree">
      {count > LARGE_TREE_THRESHOLD && (
        <div className="jcode-filetree__hint">{count} entries · deep folders collapsed</div>
      )}
      <ul className="jcode-filetree__list" role="tree">
        {roots.map((node) => (
          <TreeRow key={node.path} node={node} depth={0} initialOpenDepth={initialOpenDepth} />
        ))}
      </ul>
    </div>
  )
})

function TreeRow({
  node,
  depth,
  initialOpenDepth,
}: {
  node: TreeNode
  depth: number
  initialOpenDepth: number
}) {
  const [open, setOpen] = useState(depth < initialOpenDepth)
  const indent = { paddingLeft: `${depth * 0.85 + 0.35}rem` }

  if (!node.isDir) {
    return (
      <li role="treeitem" className="jcode-filetree__row jcode-filetree__row--file" style={indent}>
        <span className="jcode-filetree__lead" aria-hidden />
        <span className={`jcode-filetree__dot jcode-filetree__dot--${extCategory(node.name)}`} aria-hidden />
        <span className="jcode-filetree__name">{node.name}</span>
      </li>
    )
  }

  return (
    <li role="treeitem" aria-expanded={open} className="jcode-filetree__group">
      <button
        type="button"
        className="jcode-filetree__row jcode-filetree__row--dir"
        style={indent}
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronRightIcon className={`jcode-filetree__chevron${open ? ' jcode-filetree__chevron--open' : ''}`} />
        {open ? (
          <FolderOpenIcon className="jcode-filetree__folder" />
        ) : (
          <FolderIcon className="jcode-filetree__folder" />
        )}
        <span className="jcode-filetree__name jcode-filetree__name--dir">{node.name}</span>
        {!open && node.children.length > 0 && (
          <span className="jcode-filetree__badge">{node.children.length}</span>
        )}
      </button>
      {open && node.children.length > 0 && (
        <ul className="jcode-filetree__list" role="group">
          {node.children.map((child) => (
            <TreeRow
              key={child.path}
              node={child}
              depth={depth + 1}
              initialOpenDepth={initialOpenDepth}
            />
          ))}
        </ul>
      )}
    </li>
  )
}

/** Strip common list decorations and return the bare path (or ''). */
function cleanLine(line: string): string {
  let s = line.replace(/\r$/, '')
  // Drop leading tree glyphs / bullets / whitespace.
  s = s.replace(/^[\s│├└─\-*•▸▾▪]+/, '')
  s = s.trim()
  // Drop trailing annotations like "  (dir)" or size columns — keep first token
  // only when it clearly contains a path separator or looks like a filename.
  return s
}

export function parsePathList(text: string): { roots: TreeNode[]; count: number } {
  const nodes = new Map<string, TreeNode>() // full path → node
  const roots: TreeNode[] = []
  const seenLines = new Set<string>()

  const getOrCreate = (
    path: string,
    name: string,
    isDir: boolean,
    parent: TreeNode | null,
  ): TreeNode => {
    let node = nodes.get(path)
    if (!node) {
      node = { name, path, isDir, children: [] }
      nodes.set(path, node)
      if (parent) parent.children.push(node)
      else roots.push(node)
    } else if (isDir && !node.isDir) {
      node.isDir = true
    }
    return node
  }

  for (const rawLine of text.split('\n')) {
    const cleaned = cleanLine(rawLine)
    if (!cleaned || seenLines.has(cleaned)) continue
    seenLines.add(cleaned)
    const explicitDir = cleaned.endsWith('/')
    const path = cleaned.replace(/\/+$/, '')
    const segments = path.split('/').filter(Boolean)
    if (segments.length === 0) continue

    let parent: TreeNode | null = null
    let acc = ''
    segments.forEach((seg, i) => {
      acc = acc ? `${acc}/${seg}` : seg
      const isDir = i < segments.length - 1 || explicitDir
      parent = getOrCreate(acc, seg, isDir, parent)
    })
  }

  sortNodes(roots)
  return { roots, count: nodes.size }
}

/** Directories first, then case-insensitive name order (recursive). */
function sortNodes(nodes: TreeNode[]): void {
  nodes.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
    return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
  })
  for (const n of nodes) if (n.children.length > 1) sortNodes(n.children)
}

/** Map a filename to a dot color category (resolved to a token in p5.css). */
export function extCategory(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot <= 0) return 'default'
  const ext = name.slice(dot + 1).toLowerCase()
  switch (ext) {
    case 'ts':
    case 'tsx':
    case 'js':
    case 'jsx':
    case 'mjs':
    case 'cjs':
    case 'vue':
    case 'svelte':
      return 'js'
    case 'go':
      return 'go'
    case 'py':
      return 'py'
    case 'rs':
      return 'rust'
    case 'css':
    case 'scss':
    case 'sass':
    case 'less':
      return 'style'
    case 'json':
    case 'yaml':
    case 'yml':
    case 'toml':
    case 'xml':
      return 'data'
    case 'md':
    case 'mdx':
    case 'txt':
    case 'rst':
      return 'doc'
    case 'sh':
    case 'bash':
    case 'zsh':
      return 'shell'
    default:
      return 'default'
  }
}
