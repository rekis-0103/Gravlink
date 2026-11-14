import { useEffect, useMemo, useState } from 'react';
import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import css from 'highlight.js/lib/languages/css';
import go from 'highlight.js/lib/languages/go';
import ini from 'highlight.js/lib/languages/ini';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import markdown from 'highlight.js/lib/languages/markdown';
import powershell from 'highlight.js/lib/languages/powershell';
import python from 'highlight.js/lib/languages/python';
import rust from 'highlight.js/lib/languages/rust';
import sql from 'highlight.js/lib/languages/sql';
import typescript from 'highlight.js/lib/languages/typescript';
import xml from 'highlight.js/lib/languages/xml';
import yaml from 'highlight.js/lib/languages/yaml';
import 'highlight.js/styles/github-dark.css';
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  FileCode2,
  Folder,
  FolderOpen,
  RefreshCw,
  WrapText,
} from 'lucide-react';
import { api } from '../api';
import type { FileContent, TreeNode } from '../types';

hljs.registerLanguage('go', go);
hljs.registerLanguage('typescript', typescript);
hljs.registerLanguage('tsx', typescript);
hljs.registerLanguage('javascript', javascript);
hljs.registerLanguage('jsx', javascript);
hljs.registerLanguage('python', python);
hljs.registerLanguage('rust', rust);
hljs.registerLanguage('json', json);
hljs.registerLanguage('markdown', markdown);
hljs.registerLanguage('xml', xml);
hljs.registerLanguage('css', css);
hljs.registerLanguage('yaml', yaml);
hljs.registerLanguage('ini', ini);
hljs.registerLanguage('bash', bash);
hljs.registerLanguage('powershell', powershell);
hljs.registerLanguage('sql', sql);

function flattenFiles(nodes: TreeNode[]): TreeNode[] {
  const out: TreeNode[] = [];
  for (const n of nodes) {
    if (n.is_dir && n.children) {
      out.push(...flattenFiles(n.children));
    } else if (!n.is_dir) {
      out.push(n);
    }
  }
  return out;
}

function splitDirAndFile(fullPath: string): { dir: string; file: string } {
  const idx = fullPath.lastIndexOf('/');
  if (idx === -1) return { dir: '', file: fullPath };
  return { dir: fullPath.slice(0, idx + 1), file: fullPath.slice(idx + 1) };
}

function TreeBranch({
  nodes,
  depth,
  selectedPath,
  expanded,
  onToggleDir,
  onSelectFile,
}: {
  nodes: TreeNode[];
  depth: number;
  selectedPath: string;
  expanded: Record<string, boolean>;
  onToggleDir: (path: string) => void;
  onSelectFile: (path: string) => void;
}) {
  return (
    <ul className="tree-list">
      {nodes.map((node) => {
        const isOpen = !!expanded[node.path];
        return (
          <li key={node.path}>
            <button
              type="button"
              className={`tree-row ${selectedPath === node.path ? 'selected' : ''}`}
              style={{ paddingLeft: `${0.5 + depth * 0.85}rem` }}
              onClick={() => (node.is_dir ? onToggleDir(node.path) : onSelectFile(node.path))}
            >
              {node.is_dir ? (
                <>
                  {isOpen ? <ChevronDown size={15} aria-hidden="true" /> : <ChevronRight size={15} aria-hidden="true" />}
                  {isOpen ? (
                    <FolderOpen size={16} aria-hidden="true" style={{ color: 'var(--accent)', flexShrink: 0 }} />
                  ) : (
                    <Folder size={16} aria-hidden="true" style={{ color: 'var(--accent)', flexShrink: 0 }} />
                  )}
                </>
              ) : (
                <FileCode2 size={16} aria-hidden="true" style={{ color: 'var(--muted)', marginLeft: '1.1rem', flexShrink: 0 }} />
              )}
              <span className="tree-row-name">{node.name}</span>
            </button>
            {node.is_dir && isOpen && node.children && node.children.length > 0 && (
              <TreeBranch
                nodes={node.children}
                depth={depth + 1}
                selectedPath={selectedPath}
                expanded={expanded}
                onToggleDir={onToggleDir}
                onSelectFile={onSelectFile}
              />
            )}
          </li>
        );
      })}
    </ul>
  );
}

export function CodeExplorer() {
  const [nodes, setNodes] = useState<TreeNode[]>([]);
  const [rootName, setRootName] = useState('');
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [selectedFile, setSelectedFile] = useState<FileContent | null>(null);
  const [mobilePane, setMobilePane] = useState<'tree' | 'viewer'>('tree');
  const [wrapLines, setWrapLines] = useState(false);
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  const loadTree = async () => {
    setLoading(true);
    setError('');
    try {
      const res = await api.tree('', 4);
      setRootName(res.root);
      setNodes(res.nodes ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workspace tree');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadTree();
  }, []);

  const openFile = async (relPath: string) => {
    setLoading(true);
    setError('');
    try {
      const fc = await api.file(relPath);
      setSelectedFile(fc);
      setMobilePane('viewer');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to read file');
    } finally {
      setLoading(false);
    }
  };

  const searchResults = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return null;
    return flattenFiles(nodes)
      .filter((f) => f.path.toLowerCase().includes(q))
      .slice(0, 60);
  }, [nodes, query]);

  const highlightedLines = useMemo(() => {
    if (!selectedFile) return [];
    const lang =
      selectedFile.language && hljs.getLanguage(selectedFile.language) ? selectedFile.language : 'plaintext';
    const rawLines = selectedFile.content.replace(/\r\n/g, '\n').split('\n');
    // Highlight per-file for accuracy when single-line spans don't break, or fallback cleanly per line
    if (lang === 'plaintext') {
      return rawLines.map((line) => hljs.highlight(line, { language: 'plaintext' }).value || ' ');
    }
    try {
      const fullHtml = hljs.highlight(selectedFile.content.replace(/\r\n/g, '\n'), { language: lang }).value;
      // Only split directly if no multi-line span is open; otherwise per-line highlight keeps table rows balanced
      if (!fullHtml.includes('\n')) return [fullHtml || ' '];
      const split = fullHtml.split('\n');
      if (split.length === rawLines.length) {
        return split.map((l) => l || ' ');
      }
    } catch {
      // Fallback below
    }
    return rawLines.map((line) => hljs.highlight(line, { language: lang }).value || ' ');
  }, [selectedFile]);

  const copyPath = async () => {
    if (!selectedFile) return;
    try {
      await navigator.clipboard.writeText(selectedFile.path);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard unavailable in insecure context
    }
  };

  const pathParts = selectedFile ? splitDirAndFile(selectedFile.path) : null;

  return (
    <div className="code-layout">
      <div className="code-toolbar">
        <button
          type="button"
          className="compact code-mobile-toggle"
          onClick={() => setMobilePane((prev) => (prev === 'tree' ? 'viewer' : 'tree'))}
        >
          {mobilePane === 'tree' ? 'Viewer' : 'Files'}
        </button>

        <div className="code-path-label" title={selectedFile ? selectedFile.path : rootName}>
          {pathParts ? (
            <>
              <span className="path-dir">{pathParts.dir}</span>
              <span className="path-file">{pathParts.file}</span>
            </>
          ) : (
            <span className="path-dir">{rootName || 'Workspace'}</span>
          )}
        </div>

        {selectedFile && (
          <>
            <button
              type="button"
              className="compact"
              onClick={() => setWrapLines((prev) => !prev)}
              aria-label="Toggle line wrapping"
              aria-pressed={wrapLines}
            >
              <WrapText size={16} aria-hidden="true" />
            </button>
            <button type="button" className="compact" onClick={copyPath} aria-label="Copy file path">
              {copied ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
            </button>
          </>
        )}
        <button type="button" className="compact" onClick={() => void loadTree()} aria-label="Refresh file tree">
          <RefreshCw size={16} aria-hidden="true" />
        </button>
      </div>

      {error && (
        <div className="error-box" style={{ margin: '0.6rem' }} role="alert">
          {error}
        </div>
      )}

      <div className="code-panes">
        <aside className={`code-tree-pane ${mobilePane === 'viewer' ? 'hidden-mobile' : ''}`} aria-label="Workspace files">
          <div className="search-bar-wrap">
            <input
              type="search"
              className="input-box"
              enterKeyHint="search"
              placeholder="Filter files (e.g. router, App)..."
              aria-label="Filter workspace files"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <div className="scroll-area" style={{ padding: '0.45rem' }}>
            {loading && nodes.length === 0 ? (
              <p style={{ color: 'var(--muted)', padding: '0.5rem' }}>Loading workspace files...</p>
            ) : searchResults ? (
              searchResults.length === 0 ? (
                <p style={{ color: 'var(--muted)', padding: '0.5rem' }}>No matching files.</p>
              ) : (
                <ul className="tree-list">
                  {searchResults.map((f) => {
                    const parts = splitDirAndFile(f.path);
                    return (
                      <li key={f.path}>
                        <button
                          type="button"
                          className={`tree-row ${selectedFile?.path === f.path ? 'selected' : ''}`}
                          onClick={() => void openFile(f.path)}
                        >
                          <FileCode2 size={16} aria-hidden="true" style={{ color: 'var(--accent)', flexShrink: 0 }} />
                          <span className="tree-row-name">
                            <span style={{ color: 'var(--muted)' }}>{parts.dir}</span>
                            <strong>{parts.file}</strong>
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              )
            ) : nodes.length === 0 ? (
              <p style={{ color: 'var(--muted)', padding: '0.5rem' }}>No readable files found in workspace.</p>
            ) : (
              <TreeBranch
                nodes={nodes}
                depth={0}
                selectedPath={selectedFile?.path ?? ''}
                expanded={expanded}
                onToggleDir={(p) => setExpanded((prev) => ({ ...prev, [p]: !prev[p] }))}
                onSelectFile={(p) => void openFile(p)}
              />
            )}
          </div>
        </aside>

        <section
          className={`code-viewer-pane ${mobilePane === 'tree' ? 'hidden-mobile' : ''}`}
          aria-label="Code viewer"
        >
          <div className={`code-viewer-wrap ${wrapLines ? 'wrapped' : ''}`}>
            {selectedFile ? (
              <div className="code-lines" role="region" aria-label={selectedFile.path}>
                {highlightedLines.map((lineHtml, i) => (
                  <div key={i} className="code-line">
                    <span className="code-ln" aria-hidden="true">
                      {i + 1}
                    </span>
                    <code className="code-src hljs" dangerouslySetInnerHTML={{ __html: lineHtml }} />
                  </div>
                ))}
              </div>
            ) : (
              <p style={{ color: '#94a3b8', padding: '0.75rem' }}>
                Select a file from the tree to view it (read-only).
              </p>
            )}
          </div>
        </section>
      </div>
    </div>
  );
}
